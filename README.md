# IronSmith Multiplayer Lab

Laboratorio específico para el cuello de botella multiplayer de IronSmith. La
hipótesis es que podemos quitar autoridad y recuperación del navegador creador
sin portar el motor de Magic ni sumar infraestructura innecesaria.

Se ejecuta como un único binario Go con archivos locales. No requiere Durable
Objects, Redis, Postgres, Kubernetes, cuentas cloud ni suscripciones. La única
dependencia de código es una librería WebSocket sin dependencias transitivas.

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
- Comandos limitados al flujo `trusted_command` actual de IronSmith y asiento
  (`actorIndex`) derivado de la credencial, no de la afirmación del cliente.
- Diagnóstico descargable en un clic, sin tokens ni payloads ocultos.

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

Abrí `http://127.0.0.1:8080` para usar el cliente de laboratorio. Dos ventanas
permiten crear y entrar a la misma sala; el botón de desconexión prueba el
resync por cursor y el botón de diagnóstico genera un JSON listo para adjuntar
a un issue.

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
    "actorIndex": 0,
    "kind": "trusted_command",
    "payload": {
      "type": "priority_action",
      "action_ref": { "kind": "pass_priority" }
    }
  }
}
```

La API es deliberadamente pequeña. Ver [arquitectura](docs/architecture.md) y
[brechas de IronSmith](docs/ironsmith-gap-analysis.md).
