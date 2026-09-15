param(
  [string]$BaseUrl = "https://lab-multiplayer.onrender.com",
  [ValidateSet("baseline", "lab")][string]$Mode = "baseline",
  [int]$Runs = 3,
  [int]$Burst = 70
)
$ErrorActionPreference = "Stop"

function LabTest-ConnectResync($Credentials, [int]$AfterSeq) {
  $socket = [Net.WebSockets.ClientWebSocket]::new()
  $url = $BaseUrl.Replace("https://", "wss://").Replace("http://", "ws://") + "/v1/ws"
  $socket.ConnectAsync([Uri]$url, [Threading.CancellationToken]::None).GetAwaiter().GetResult() | Out-Null
  $hello = @{ type = "resume"; protocol = 1; roomId = $Credentials.roomId; playerId = $Credentials.playerId; resumeToken = $Credentials.resumeToken; afterSeq = $AfterSeq; mode = $Mode } | ConvertTo-Json -Compress
  $bytes = [Text.Encoding]::UTF8.GetBytes($hello)
  $socket.SendAsync([ArraySegment[byte]]::new($bytes), [Net.WebSockets.WebSocketMessageType]::Text, $true, [Threading.CancellationToken]::None).GetAwaiter().GetResult() | Out-Null
  return $socket
}
function LabTest-ReadResync($Socket, [int]$TimeoutSeconds = 12) {
  $cancel = [Threading.CancellationTokenSource]::new([TimeSpan]::FromSeconds($TimeoutSeconds))
  try {
    $buffer = New-Object byte[] 65536; $parts = [Text.StringBuilder]::new()
    do { $part = $Socket.ReceiveAsync([ArraySegment[byte]]::new($buffer), $cancel.Token).GetAwaiter().GetResult(); [void]$parts.Append([Text.Encoding]::UTF8.GetString($buffer, 0, $part.Count)) } while (-not $part.EndOfMessage)
    return $parts.ToString() | ConvertFrom-Json
  } finally { $cancel.Dispose() }
}
function LabTest-ReadFirstResyncFragment($Socket) {
  $cancel = [Threading.CancellationTokenSource]::new([TimeSpan]::FromSeconds(8))
  try {
    $buffer = New-Object byte[] 65536
    return $Socket.ReceiveAsync([ArraySegment[byte]]::new($buffer), $cancel.Token).GetAwaiter().GetResult()
  } finally { $cancel.Dispose() }
}
function LabTest-SendResync($Socket, [string]$Id, [uint64]$Seq, [int]$Actor, [string]$Prefix) {
  $message = @{ type = "trusted_command"; action = @{ commandId = $Id; seq = $Seq; expectedSeq = $Seq; actorIndex = $Actor; kind = "trusted_command"; command = @{ type = "priority_action"; action_ref = @{ kind = "disconnect_resync_test" } }; prefixHash = $Prefix } } | ConvertTo-Json -Depth 10 -Compress
  $bytes = [Text.Encoding]::UTF8.GetBytes($message)
  $Socket.SendAsync([ArraySegment[byte]]::new($bytes), [Net.WebSockets.WebSocketMessageType]::Text, $true, [Threading.CancellationToken]::None).GetAwaiter().GetResult() | Out-Null
}
function LabTest-CloseResync($Socket) { try { $Socket.CloseAsync([Net.WebSockets.WebSocketCloseStatus]::NormalClosure, "resync disconnect", [Threading.CancellationToken]::None).GetAwaiter().GetResult() | Out-Null } catch { $Socket.Abort() }; $Socket.Dispose() }
function LabTest-RunResync([int]$Run) {
  $room = Invoke-RestMethod "$BaseUrl/v1/rooms" -Method Post -ContentType "application/json" -Body '{"name":"A"}'
  $guest = Invoke-RestMethod "$BaseUrl/v1/rooms/$($room.roomId)/players" -Method Post -ContentType "application/json" -Body '{"name":"B"}'
  $hostSocket = LabTest-ConnectResync $room 0; $guestSocket = LabTest-ConnectResync $guest 0
  $null = LabTest-ReadResync $hostSocket; $null = LabTest-ReadResync $guestSocket
  $prefix = ""; $first = $null
  for($i=0; $i -lt $Burst; $i++) { LabTest-SendResync $hostSocket "resync-$Run-$i" $i $room.playerIndex $prefix; $first = LabTest-ReadResync $hostSocket; $prefix = $first.event.prefixHash }
  LabTest-CloseResync $guestSocket
  $recovering = LabTest-ConnectResync $guest 1
  $fragment = LabTest-ReadFirstResyncFragment $recovering
  $fragmentBytes = $fragment.Count
  LabTest-CloseResync $recovering
  LabTest-SendResync $hostSocket "resync-$Run-$Burst" $Burst $room.playerIndex $prefix
  $last = LabTest-ReadResync $hostSocket
  $final = LabTest-ConnectResync $guest 1
  $resumed = LabTest-ReadResync $final
  $events = @($resumed.events | Where-Object { $null -ne $_ })
  $result = [ordered]@{ mode=$Mode; run=$Run; roomId=$room.roomId; seqBeforeResync=$Burst; serverSeq=$last.event.seq; firstFragmentBytes=$fragmentBytes; replayCount=$events.Count; replayLastSeq=$events[-1].seq; prefixHash=$events[-1].prefixHash }
  LabTest-CloseResync $hostSocket; LabTest-CloseResync $final; return $result
}
1..$Runs | ForEach-Object { LabTest-RunResync $_ }
