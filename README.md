# IronSmith Multiplayer Lab

Laboratorio aislado para demostrar una sesión multiplayer que no depende de que
el navegador creador siga vivo. No porta el motor de Magic: resuelve primero el
problema de transporte, identidad, orden, durabilidad y recuperación.

## Qué demuestra hoy

- Identidad estable (`playerId` + `resumeToken`) separada del WebSocket.
- Secuencia monotónica asignada por el servidor.
- Acciones idempotentes mediante `actionId`.
- Control de concurrencia mediante `expectedSeq`.
- Journal durable `fsync` antes del broadcast y cadena SHA-256 detectable.
- Reconexión por cursor: `afterSeq` devuelve sólo el tramo faltante.
- Backpressure acotado: un cliente lento se desconecta sin congelar la sala.
- El jugador creador puede desaparecer y el resto continúa enviando acciones.
- Reinicio del proceso sin perder sala, asientos ni transcript.

## Ejecutar

Requiere Go 1.23 o posterior.

```powershell
go mod download
go test ./...
go run ./cmd/server
```

El workflow de CI ejecuta además `go test -race ./...` en Linux. En Windows el
race detector de Go requiere un compilador C compatible.

El servidor escucha en `127.0.0.1:8080` y guarda datos en `./data`. Variables:

- `LISTEN_ADDR`
- `DATA_DIR`
- `ALLOWED_ORIGINS` (orígenes exactos separados por coma)

## Flujo mínimo

1. `POST /v1/rooms` con `{ "name": "Fiamma" }`.
2. `POST /v1/rooms/{roomId}/players` para cada invitado.
3. Abrir `GET /v1/ws` y mandar como primer frame:

```json
{
  "type": "resume",
  "protocol": 1,
  "roomId": "room-...",
  "playerId": "player-...",
  "resumeToken": "...",
  "afterSeq": 0
}
```

4. Enviar acciones reintentables:

```json
{
  "type": "action",
  "action": {
    "actionId": "action-00000001",
    "expectedSeq": 0,
    "kind": "pass_priority",
    "payload": { "seat": 0 }
  }
}
```

La API es deliberadamente pequeña. Ver [arquitectura](docs/architecture.md) y
[brechas de IronSmith](docs/ironsmith-gap-analysis.md).
