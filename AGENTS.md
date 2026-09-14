# IronSmith Multiplayer Lab
Laboratorio autocontenido para reproducir y endurecer las sesiones multiplayer de IronSmith.
La fuente de verdad es FiammaMuscari/ironsmith@main; comparar chiplis/ironsmith sólo para origen.
El lab contiene mejoras experimentales y no equivale automáticamente al comportamiento real.
IronSmith real conserva el motor Rust/WASM, el estado y las reglas del juego.
Arquitectura: cliente -> WebSocket Go -> sala/secuenciador -> journal JSONL -> replay/resume.
Go valida identidad, ordena acciones, asigna seq autoritativa, persiste y retransmite.
Las identidades sobreviven a la conexión mediante resume token; los duplicados son idempotentes.
El replay por cursor, la recuperación tras restart, la protección ante clientes lentos y diagnósticos ya funcionan.
El adaptador representa trusted_command/apply_action, IDs, seq, actorIndex y prefixHash sin ejecutar reglas.
Archivos: internal/protocol, internal/session, internal/store, internal/server, cmd/server, docs.
Ejecutar: `go run ./cmd/server`; tests: `go test ./...`.
No agregar infraestructura externa mientras no sea necesaria.
Workflow: leer este archivo, una feature, tests focalizados, `go test ./...`, commit pequeño, push a main y STOP.
Próximo paso exacto: comparar baseline del fork contra el lab y reproducir un fallo concreto antes de mejorar.
