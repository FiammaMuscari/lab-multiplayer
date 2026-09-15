param(
  [string]$BaseUrl = "https://lab-multiplayer.onrender.com",
  [ValidateSet("baseline", "lab")][string]$Mode = "baseline",
  [int]$Runs = 1,
  [int]$Burst = 70
)

$ErrorActionPreference = "Stop"

function LabTest-ConnectSlow($Credentials, [int]$AfterSeq) {
  $socket = [Net.WebSockets.ClientWebSocket]::new()
  $wsUrl = $BaseUrl.Replace("https://", "wss://").Replace("http://", "ws://") + "/v1/ws"
  $socket.ConnectAsync([Uri]$wsUrl, [Threading.CancellationToken]::None).GetAwaiter().GetResult() | Out-Null
  $hello = @{ type = "resume"; protocol = 1; roomId = $Credentials.roomId; playerId = $Credentials.playerId; resumeToken = $Credentials.resumeToken; afterSeq = $AfterSeq; mode = $Mode } | ConvertTo-Json -Compress
  $bytes = [Text.Encoding]::UTF8.GetBytes($hello)
  $socket.SendAsync([ArraySegment[byte]]::new($bytes), [Net.WebSockets.WebSocketMessageType]::Text, $true, [Threading.CancellationToken]::None).GetAwaiter().GetResult() | Out-Null
  return @{ Socket = $socket; Hello = LabTest-ReadSlow $socket }
}

function LabTest-ReadSlow($Socket, [int]$TimeoutSeconds = 10) {
  $cancel = [Threading.CancellationTokenSource]::new([TimeSpan]::FromSeconds($TimeoutSeconds))
  try {
    $buffer = New-Object byte[] 65536
    $parts = [Text.StringBuilder]::new()
    do {
      $result = $Socket.ReceiveAsync([ArraySegment[byte]]::new($buffer), $cancel.Token).GetAwaiter().GetResult()
      [void]$parts.Append([Text.Encoding]::UTF8.GetString($buffer, 0, $result.Count))
    } while (-not $result.EndOfMessage)
    return $parts.ToString() | ConvertFrom-Json
  } finally {
    $cancel.Dispose()
  }
}

function LabTest-SendSlow($Socket, [string]$CommandId, [uint64]$Sequence, [int]$ActorIndex, [string]$PrefixHash) {
  $message = @{ type = "trusted_command"; action = @{ commandId = $CommandId; seq = $Sequence; expectedSeq = $Sequence; actorIndex = $ActorIndex; kind = "trusted_command"; command = @{ type = "priority_action"; action_ref = @{ kind = "slow_client_test" } }; prefixHash = $PrefixHash } } | ConvertTo-Json -Depth 10 -Compress
  $bytes = [Text.Encoding]::UTF8.GetBytes($message)
  $Socket.SendAsync([ArraySegment[byte]]::new($bytes), [Net.WebSockets.WebSocketMessageType]::Text, $true, [Threading.CancellationToken]::None).GetAwaiter().GetResult() | Out-Null
}

function LabTest-DisconnectSlow($Socket) {
  try { $Socket.CloseAsync([Net.WebSockets.WebSocketCloseStatus]::NormalClosure, "slow client test", [Threading.CancellationToken]::None).GetAwaiter().GetResult() | Out-Null } catch { $Socket.Abort() }
  $Socket.Dispose()
}

function LabTest-RunSlow([int]$RunNumber) {
  $room = Invoke-RestMethod "$BaseUrl/v1/rooms" -Method Post -ContentType "application/json" -Body '{"name":"A"}'
  $guest = Invoke-RestMethod "$BaseUrl/v1/rooms/$($room.roomId)/players" -Method Post -ContentType "application/json" -Body '{"name":"B"}'
  $hostConnection = LabTest-ConnectSlow $room 0
  $guestConnection = LabTest-ConnectSlow $guest 0
  $prefix = ""
  $last = $null
  for ($index = 0; $index -lt $Burst; $index++) {
    LabTest-SendSlow $hostConnection.Socket "slow-$RunNumber-$index" $index $room.playerIndex $prefix
    $last = LabTest-ReadSlow $hostConnection.Socket
    if ($last.type -ne "apply_action") { throw "host did not receive apply_action at burst index $index" }
    $prefix = $last.event.prefixHash
  }
  $diagnostic = Invoke-RestMethod "$BaseUrl/v1/rooms/$($room.roomId)/diagnostics" -Method Post -ContentType "application/json" -Body (ConvertTo-Json @{ playerId = $room.playerId; resumeToken = $room.resumeToken })
  LabTest-DisconnectSlow $guestConnection.Socket
  $reconnected = LabTest-ConnectSlow $guest 0
  $replay = @($reconnected.Hello.events | Where-Object { $null -ne $_ })
  $result = [ordered]@{
    mode = $Mode; run = $RunNumber; roomId = $room.roomId; burst = $Burst
    hostSeq = $last.event.seq; replayCount = $replay.Count; replayLastSeq = $replay[-1].seq
    diagnosticsSeq = $diagnostic.room.currentSeq; slowDisconnects = $diagnostic.room.metrics.slowDisconnects
    conflicts = $diagnostic.room.metrics.conflicts
  }
  LabTest-DisconnectSlow $hostConnection.Socket
  LabTest-DisconnectSlow $reconnected.Socket
  return $result
}

1..$Runs | ForEach-Object { LabTest-RunSlow $_ }
