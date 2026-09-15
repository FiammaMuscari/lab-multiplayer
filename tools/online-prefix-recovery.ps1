param(
  [string]$BaseUrl = "https://lab-multiplayer.onrender.com",
  [ValidateSet("baseline", "lab")][string]$Mode = "baseline",
  [int]$Runs = 3
)
$ErrorActionPreference = "Stop"
function LabTest-ConnectPrefix($Credentials) {
  $socket = [Net.WebSockets.ClientWebSocket]::new(); $url = $BaseUrl.Replace("https://", "wss://").Replace("http://", "ws://") + "/v1/ws"
  $socket.ConnectAsync([Uri]$url, [Threading.CancellationToken]::None).GetAwaiter().GetResult() | Out-Null
  $hello = @{ type="resume"; protocol=1; roomId=$Credentials.roomId; playerId=$Credentials.playerId; resumeToken=$Credentials.resumeToken; afterSeq=0; mode=$Mode } | ConvertTo-Json -Compress
  $bytes=[Text.Encoding]::UTF8.GetBytes($hello); $socket.SendAsync([ArraySegment[byte]]::new($bytes),[Net.WebSockets.WebSocketMessageType]::Text,$true,[Threading.CancellationToken]::None).GetAwaiter().GetResult() | Out-Null
  return $socket
}
function LabTest-ReadPrefix($Socket) {
  $cancel=[Threading.CancellationTokenSource]::new([TimeSpan]::FromSeconds(10)); try { $buffer=New-Object byte[] 65536; $parts=[Text.StringBuilder]::new(); do { $part=$Socket.ReceiveAsync([ArraySegment[byte]]::new($buffer),$cancel.Token).GetAwaiter().GetResult(); [void]$parts.Append([Text.Encoding]::UTF8.GetString($buffer,0,$part.Count)) } while(-not $part.EndOfMessage); return $parts.ToString()|ConvertFrom-Json } finally { $cancel.Dispose() }
}
function LabTest-SendPrefix($Socket,[string]$Id,[uint64]$Seq,[int]$Actor,[string]$Prefix) { $m=@{type="trusted_command";action=@{commandId=$Id;seq=$Seq;expectedSeq=$Seq;actorIndex=$Actor;kind="trusted_command";command=@{type="priority_action";action_ref=@{kind="prefix_recovery_test"}};prefixHash=$Prefix}}|ConvertTo-Json -Depth 10 -Compress; $b=[Text.Encoding]::UTF8.GetBytes($m); $Socket.SendAsync([ArraySegment[byte]]::new($b),[Net.WebSockets.WebSocketMessageType]::Text,$true,[Threading.CancellationToken]::None).GetAwaiter().GetResult()|Out-Null }
function LabTest-ClosePrefix($Socket) { try {$Socket.CloseAsync([Net.WebSockets.WebSocketCloseStatus]::NormalClosure,"prefix test",[Threading.CancellationToken]::None).GetAwaiter().GetResult()|Out-Null}catch{$Socket.Abort()};$Socket.Dispose() }
function LabTest-RunPrefix([int]$Run) {
  $room=Invoke-RestMethod "$BaseUrl/v1/rooms" -Method Post -ContentType "application/json" -Body '{"name":"A"}'; $guest=Invoke-RestMethod "$BaseUrl/v1/rooms/$($room.roomId)/players" -Method Post -ContentType "application/json" -Body '{"name":"B"}'
  $a=LabTest-ConnectPrefix $room; $b=LabTest-ConnectPrefix $guest; $null=LabTest-ReadPrefix $a; $null=LabTest-ReadPrefix $b
  LabTest-SendPrefix $a "prefix-$Run-1" 0 $room.playerIndex ""; $a1=LabTest-ReadPrefix $a; $null=LabTest-ReadPrefix $b
  LabTest-SendPrefix $b "prefix-$Run-bad" 1 $guest.playerIndex "bad-prefix"; $resync=LabTest-ReadPrefix $b
  LabTest-SendPrefix $b "prefix-$Run-2" 1 $guest.playerIndex $a1.event.prefixHash; $recovered=LabTest-ReadPrefix $b
  $diag=Invoke-RestMethod "$BaseUrl/v1/rooms/$($room.roomId)/diagnostics" -Method Post -ContentType "application/json" -Body (ConvertTo-Json @{playerId=$guest.playerId;resumeToken=$guest.resumeToken})
  $result=[ordered]@{mode=$Mode;run=$Run;roomId=$room.roomId;resyncType=$resync.type;divergence=$resync.divergence;recoveredSeq=$recovered.event.seq;diagnosticsSeq=$diag.room.currentSeq;divergences=$diag.room.metrics.divergences;conflicts=$diag.room.metrics.conflicts}
  LabTest-ClosePrefix $a; LabTest-ClosePrefix $b; return $result
}
1..$Runs|ForEach-Object{LabTest-RunPrefix $_}
