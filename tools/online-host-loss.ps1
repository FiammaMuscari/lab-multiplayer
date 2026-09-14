param(
  [string]$BaseUrl = "https://lab-multiplayer.onrender.com",
  [ValidateSet("baseline", "lab")][string]$Mode = "baseline",
  [int]$Runs = 3
)

$ErrorActionPreference = "Stop"

function LabTest-Connect($Credentials, [int]$AfterSeq) {
  $socket = [Net.WebSockets.ClientWebSocket]::new()
  $wsUrl = $BaseUrl.Replace("https://", "wss://").Replace("http://", "ws://") + "/v1/ws"
  $socket.ConnectAsync([Uri]$wsUrl, [Threading.CancellationToken]::None).GetAwaiter().GetResult() | Out-Null
  $hello = @{ type = "resume"; protocol = 1; roomId = $Credentials.roomId; playerId = $Credentials.playerId; resumeToken = $Credentials.resumeToken; afterSeq = $AfterSeq; mode = $Mode } | ConvertTo-Json -Compress
  $bytes = [Text.Encoding]::UTF8.GetBytes($hello)
  $socket.SendAsync([ArraySegment[byte]]::new($bytes), [Net.WebSockets.WebSocketMessageType]::Text, $true, [Threading.CancellationToken]::None).GetAwaiter().GetResult() | Out-Null
  $message = LabTest-Read $socket
  return @{ Socket = $socket; Hello = $message }
}

function LabTest-Read($Socket, [int]$TimeoutSeconds = 8) {
  $cancel = [Threading.CancellationTokenSource]::new([TimeSpan]::FromSeconds($TimeoutSeconds))
  try {
    $buffer = New-Object byte[] 65536
    $result = $Socket.ReceiveAsync([ArraySegment[byte]]::new($buffer), $cancel.Token).GetAwaiter().GetResult()
    return [Text.Encoding]::UTF8.GetString($buffer, 0, $result.Count) | ConvertFrom-Json
  } finally {
    $cancel.Dispose()
  }
}

function LabTest-Send($Socket, [string]$CommandId, [uint64]$Sequence, [int]$ActorIndex, [string]$PrefixHash) {
  $message = @{ type = "trusted_command"; action = @{ commandId = $CommandId; seq = $Sequence; expectedSeq = $Sequence; actorIndex = $ActorIndex; kind = "trusted_command"; command = @{ type = "priority_action"; action_ref = @{ kind = "host_loss_test" } }; prefixHash = $PrefixHash } } | ConvertTo-Json -Depth 10 -Compress
  $bytes = [Text.Encoding]::UTF8.GetBytes($message)
  $Socket.SendAsync([ArraySegment[byte]]::new($bytes), [Net.WebSockets.WebSocketMessageType]::Text, $true, [Threading.CancellationToken]::None).GetAwaiter().GetResult() | Out-Null
}

function LabTest-Disconnect($Socket) {
  try { $Socket.CloseAsync([Net.WebSockets.WebSocketCloseStatus]::NormalClosure, "test disconnect", [Threading.CancellationToken]::None).GetAwaiter().GetResult() | Out-Null } catch { $Socket.Abort() }
  $Socket.Dispose()
}

function LabTest-WaitHostOffline($Credentials) {
  for ($attempt = 0; $attempt -lt 15; $attempt++) {
    try {
      $probe = Invoke-RestMethod "$BaseUrl/v1/rooms/$($Credentials.roomId)/diagnostics" -Method Post -ContentType "application/json" -Body (ConvertTo-Json @{ playerId = $Credentials.playerId; resumeToken = $Credentials.resumeToken })
      if ($probe.room.activeConnections -le 1) { break }
    } catch { }
    Start-Sleep -Seconds 1
  }
}

function LabTest-Run([int]$RunNumber) {
  $room = Invoke-RestMethod "$BaseUrl/v1/rooms" -Method Post -ContentType "application/json" -Body '{"name":"A"}'
  $guest = Invoke-RestMethod "$BaseUrl/v1/rooms/$($room.roomId)/players" -Method Post -ContentType "application/json" -Body '{"name":"B"}'
  $hostConnection = LabTest-Connect $room 0
  $guestConnection = LabTest-Connect $guest 0
  LabTest-Send $hostConnection.Socket "host-$RunNumber-1" 0 $room.playerIndex ""
  $hostEvent = LabTest-Read $hostConnection.Socket
  $guestEvent = LabTest-Read $guestConnection.Socket
  LabTest-Send $guestConnection.Socket "guest-$RunNumber-2" 1 $guest.playerIndex $hostEvent.event.prefixHash
  $guestSecond = LabTest-Read $guestConnection.Socket
  $null = LabTest-Read $hostConnection.Socket
  LabTest-Disconnect $hostConnection.Socket
  LabTest-WaitHostOffline $guest
  LabTest-Send $guestConnection.Socket "guest-$RunNumber-3" 2 $guest.playerIndex $guestSecond.event.prefixHash
  $afterLoss = LabTest-Read $guestConnection.Socket
  $reconnected = LabTest-Connect $room 2
  $diagnostic = Invoke-RestMethod "$BaseUrl/v1/rooms/$($room.roomId)/diagnostics" -Method Post -ContentType "application/json" -Body (ConvertTo-Json @{ playerId = $guest.playerId; resumeToken = $guest.resumeToken })
  $result = [ordered]@{
    mode = $Mode; run = $RunNumber; roomId = $room.roomId
    afterHostLoss = $afterLoss.type; errorCode = $afterLoss.code
    serverSeqAfter = $afterLoss.currentSeq; replayCount = @($reconnected.Hello.events).Count
    guestSeqBeforeLoss = $guestSecond.event.seq; guestPrefixHash = $guestSecond.event.prefixHash
    diagnosticsSeq = $diagnostic.room.currentSeq; conflicts = $diagnostic.room.metrics.conflicts
  }
  LabTest-Disconnect $guestConnection.Socket
  LabTest-Disconnect $reconnected.Socket
  return $result
}

1..$Runs | ForEach-Object { LabTest-Run $_ }
