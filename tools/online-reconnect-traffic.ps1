param(
  [string]$BaseUrl = "https://lab-multiplayer.onrender.com",
  [ValidateSet("baseline", "lab")][string]$Mode = "baseline",
  [int]$Runs = 3
)

$ErrorActionPreference = "Stop"

function LabTest-ConnectTraffic($Credentials, [int]$AfterSeq) {
  $socket = [Net.WebSockets.ClientWebSocket]::new()
  $wsUrl = $BaseUrl.Replace("https://", "wss://").Replace("http://", "ws://") + "/v1/ws"
  $socket.ConnectAsync([Uri]$wsUrl, [Threading.CancellationToken]::None).GetAwaiter().GetResult() | Out-Null
  $hello = @{ type = "resume"; protocol = 1; roomId = $Credentials.roomId; playerId = $Credentials.playerId; resumeToken = $Credentials.resumeToken; afterSeq = $AfterSeq; mode = $Mode } | ConvertTo-Json -Compress
  $bytes = [Text.Encoding]::UTF8.GetBytes($hello)
  $socket.SendAsync([ArraySegment[byte]]::new($bytes), [Net.WebSockets.WebSocketMessageType]::Text, $true, [Threading.CancellationToken]::None).GetAwaiter().GetResult() | Out-Null
  return @{ Socket = $socket; Hello = LabTest-ReadTraffic $socket }
}

function LabTest-ReadTraffic($Socket, [int]$TimeoutSeconds = 8) {
  $cancel = [Threading.CancellationTokenSource]::new([TimeSpan]::FromSeconds($TimeoutSeconds))
  try {
    $buffer = New-Object byte[] 65536
    $result = $Socket.ReceiveAsync([ArraySegment[byte]]::new($buffer), $cancel.Token).GetAwaiter().GetResult()
    return [Text.Encoding]::UTF8.GetString($buffer, 0, $result.Count) | ConvertFrom-Json
  } finally {
    $cancel.Dispose()
  }
}

function LabTest-SendTraffic($Socket, [string]$CommandId, [uint64]$Sequence, [int]$ActorIndex, [string]$PrefixHash) {
  $message = @{ type = "trusted_command"; action = @{ commandId = $CommandId; seq = $Sequence; expectedSeq = $Sequence; actorIndex = $ActorIndex; kind = "trusted_command"; command = @{ type = "priority_action"; action_ref = @{ kind = "reconnect_traffic_test" } }; prefixHash = $PrefixHash } } | ConvertTo-Json -Depth 10 -Compress
  $bytes = [Text.Encoding]::UTF8.GetBytes($message)
  $Socket.SendAsync([ArraySegment[byte]]::new($bytes), [Net.WebSockets.WebSocketMessageType]::Text, $true, [Threading.CancellationToken]::None).GetAwaiter().GetResult() | Out-Null
}

function LabTest-DisconnectTraffic($Socket) {
  try { $Socket.CloseAsync([Net.WebSockets.WebSocketCloseStatus]::NormalClosure, "traffic reconnect", [Threading.CancellationToken]::None).GetAwaiter().GetResult() | Out-Null } catch { $Socket.Abort() }
  $Socket.Dispose()
}

function LabTest-RunTraffic([int]$RunNumber) {
  $room = Invoke-RestMethod "$BaseUrl/v1/rooms" -Method Post -ContentType "application/json" -Body '{"name":"A"}'
  $guest = Invoke-RestMethod "$BaseUrl/v1/rooms/$($room.roomId)/players" -Method Post -ContentType "application/json" -Body '{"name":"B"}'
  $hostConnection = LabTest-ConnectTraffic $room 0
  $guestConnection = LabTest-ConnectTraffic $guest 0

  LabTest-SendTraffic $hostConnection.Socket "traffic-$RunNumber-1" 0 $room.playerIndex ""
  $hostFirst = LabTest-ReadTraffic $hostConnection.Socket
  $guestFirst = LabTest-ReadTraffic $guestConnection.Socket
  LabTest-DisconnectTraffic $guestConnection.Socket

  LabTest-SendTraffic $hostConnection.Socket "traffic-$RunNumber-2" 1 $room.playerIndex $hostFirst.event.prefixHash
  $hostSecond = LabTest-ReadTraffic $hostConnection.Socket
  LabTest-SendTraffic $hostConnection.Socket "traffic-$RunNumber-3" 2 $room.playerIndex $hostSecond.event.prefixHash
  $hostThird = LabTest-ReadTraffic $hostConnection.Socket

  $reconnected = LabTest-ConnectTraffic $guest 1
  $replay = @($reconnected.Hello.events | Where-Object { $null -ne $_ })
  $diagnostic = Invoke-RestMethod "$BaseUrl/v1/rooms/$($guest.roomId)/diagnostics" -Method Post -ContentType "application/json" -Body (ConvertTo-Json @{ playerId = $guest.playerId; resumeToken = $guest.resumeToken })
  $result = [ordered]@{
    mode = $Mode; run = $RunNumber; roomId = $room.roomId
    guestDisconnectedDuringTraffic = $true; hostSeq = $hostThird.event.seq
    replayCount = $replay.Count; replaySeqs = @($replay | ForEach-Object { $_.seq })
    diagnosticsSeq = $diagnostic.room.currentSeq; conflicts = $diagnostic.room.metrics.conflicts
  }
  LabTest-DisconnectTraffic $hostConnection.Socket
  LabTest-DisconnectTraffic $reconnected.Socket
  return $result
}

1..$Runs | ForEach-Object { LabTest-RunTraffic $_ }
