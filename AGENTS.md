# IronSmith Multiplayer Lab
Laboratorio autocontenido para reproducir y endurecer las sesiones multiplayer de IronSmith.
IronSmith real conserva el motor Rust/WASM, el estado y las reglas del juego.
Arquitectura: cliente -> WebSocket Go -> sala/secuenciador -> journal JSONL -> replay/resume.
Go valida identidad, ordena acciones, asigna seq autoritativa, persiste y retransmite.
Las identidades sobreviven a la conexión mediante resume token; los duplicados son idempotentes.
El replay por cursor, la recuperación tras restart, la protección ante clientes lentos y diagnósticos ya funcionan.
El protocolo aún usa acciones genéricas; falta adaptar fielmente trusted_command/apply_action y sus metadatos.
Archivos: internal/protocol, internal/session, internal/store, internal/server, cmd/server, docs.
Ejecutar: `go run ./cmd/server`; tests: `go test ./...`.
No agregar infraestructura externa mientras no sea necesaria.
Workflow: leer este archivo, una feature, tests focalizados, `go test ./...`, commit pequeño, push a main y STOP.
Próximo paso exacto: implementar el adaptador mínimo del protocolo real y sus pruebas de secuencia/hash.
