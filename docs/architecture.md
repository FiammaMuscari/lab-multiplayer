# Arquitectura del laboratorio

## Invariantes

1. Un socket es efímero; un asiento no.
2. Una acción aceptada tiene una única secuencia global para la sala.
3. Reintentar el mismo `actionId` nunca duplica el efecto.
4. El evento se persiste y sincroniza a disco antes de ser visible.
5. Un consumidor lento se aísla; jamás bloquea el commit de la sala.
6. El journal se valida completo al iniciar: secuencia, `prevHash` y `hash`.
7. Desconectar al creador no cambia la autoridad del servidor.

## Camino de una acción

```text
cliente -> autenticación de asiento -> expectedSeq -> deduplicación
       -> append JSONL + fsync -> commit en memoria -> broadcast acotado
```

Si el proceso cae después de `fsync` pero antes del broadcast, al reconectar el
cliente recibe el evento por `afterSeq`. Si el ACK o broadcast se pierde, el
cliente reintenta el mismo `actionId` y recibe la secuencia ya asignada.

## Modelo de concurrencia

Cada `Room` serializa cambios con un mutex breve. La E/S de cada WebSocket tiene
su propio lector y escritor. Los subscribers tienen un buffer fijo; llenarlo
elimina sólo a ese subscriber. Esto elimina el patrón donde un peer en resync
mantiene una barrera global sobre las acciones de los demás.

El replay y la suscripción en vivo se toman dentro de la misma sección crítica.
Esto evita la carrera sutil donde una acción podía caer exactamente entre ambos
pasos y dejar al cliente esperando para siempre.

## Durabilidad

Cada sala contiene:

```text
data/<roomId>/meta.json
data/<roomId>/events.jsonl
```

`meta.json` guarda hashes de tokens, nunca tokens recuperables. `events.jsonl`
es append-only y cada línea encadena el hash anterior. La versión actual no
compacta: conservar el transcript completo hace que los experimentos de resync
sean auditables y evita confiar prematuramente en snapshots producidos por un
navegador.

## Lo que falta antes de producción

- Autenticación/códigos de invitación y rate limiting distribuido.
- Base transaccional (SQLite/Postgres) para varias réplicas.
- Snapshots verificables del motor y política de compacción.
- Cifrado/redacción de información privada por asiento.
- Adaptador del protocolo real de IronSmith y versionado de migraciones.
- Métricas OpenTelemetry, límites por IP/cuenta y protección antiabuso.
- TLS/reverse proxy, backups y pruebas de restore.

Es intencional: este repo valida primero que la sesión sobreviva a sockets,
pestañas y al navegador creador sin mezclar todavía reglas de Magic.

## Diagnóstico de un clic

`POST /v1/rooms/{roomId}/diagnostics` exige las credenciales del asiento y
devuelve sólo metadatos: secuencia, conexiones, conflictos, duplicados,
reconexiones, clientes lentos, hashes y veinte eventos sin payload. El cliente
web combina eso con su cursor, visibilidad de pestaña y estado del socket.
Nunca incluye el resume token ni comandos que puedan revelar cartas ocultas.

## Sobre IronSmith

El adaptador conserva el sobre real: `trusted_command` entra con `commandId`,
`seq` informativa, `actorIndex`, `command` opaco y `prefixHash`; la sala emite
`apply_action` con el `seq` autoritativo, el mismo `commandId` y su prefijo.
Go sólo valida, ordena, deduplica, persiste y reproduce. Un prefijo divergente
produce `state_resync` con el transcript; el motor Rust/WASM debe decidir cómo
reconstruir el estado del juego.
