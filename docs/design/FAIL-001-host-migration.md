# FAIL-001: diseño mínimo de continuidad del host

Estado de referencia: `FiammaMuscari/ironsmith@main`. Este documento es
diseño técnico; no cambia IronSmith ni el laboratorio.

## Estado actual

Los invitados ya reciben el transcript aceptado (`actionHistoryRef`),
`lastAppliedSequence`, `prefixHash`, roster y el payload de inicio. Cada
cliente también mantiene su propio estado WASM, pero los checkpoints enviados
por el host pueden ser redacted por asiento.

Sólo el host actual posee de forma suficiente para aceptar comandos:

- `role === "host"` y la cola de secuenciación de `trusted-sequencer.js`;
- el estado WASM completo y el cálculo de auditoría del host;
- `auditKeyPairRef`, conexiones de clientes, entregas pendientes y colas de
  comandos;
- el checkpoint/transcript persistido en IndexedDB por `relay/session.js`.

El relay conserva la identidad/token de cada peer y un `config.host` fijo en
`web/relay/src/worker.js`. Si el host queda offline, no autoriza una identidad
nueva a entrar y emite `offline`; no existe un lease o epoch transferible.

## Bloqueo de promoción

`promoteLocalPlayerToHost` devuelve inmediatamente `false` cuando
`isRelayId(lobbyId)` es verdadero. Esa condición evita que un invitado intente
reclamar un ID de relay que el worker todavía considera propiedad del host
original. El worker además rechaza el alta de un peer nuevo cuando
`config.host` no está conectado. Quitar sólo el `return false` produciría dos
autoridades potenciales y no transferiría el estado del juego.

## Opciones

### A. Migración entre peers

Es la opción conceptualmente menor. Requiere un claim determinista (menor
`player.index` conectado), un lease/epoch monotónico en el relay, fencing del
host anterior, y un checkpoint autoritativo completo antes de aceptar comandos.
También debe resolver comandos pendientes y prefijos divergentes. El estado
actual no garantiza que un invitado tenga ese checkpoint completo tras una
caída abrupta, por lo que A no es segura todavía para partidas relay.

### B. Secuenciador pequeño en el relay

El relay podría asignar `seq`, `commandId` y `prefixHash`, pero seguiría sin
tener reglas WASM, estado privado ni validación completa. Ordenar comandos no
transfiere la autoridad de estado y cambia el protocolo de forma sustancial.

### C. Autoridad persistente estilo LAB

Resuelve la continuidad almacenando journal y estado fuera de los peers, pero
requiere portar/exportar estado del motor y agregar infraestructura de
persistencia. No es un parche pequeño ni compatible con el objetivo actual.

## Recomendación

Elegir A sólo como migración condicionada y por etapas; no eliminar aún el
bloqueo relay. El primer bloque seguro debe ser un contrato de autoridad, no la
promoción:

1. Definir `authorityEpoch`, `currentHostPeerId` y `currentHostPlayerIndex` en
   el payload de partida.
2. Hacer que el relay otorgue un único lease al candidato determinista sólo
   cuando el host anterior esté fenced/offline.
3. Exigir un checkpoint completo verificable (o rechazar la promoción) y
   validar que su `lastSequence` y `prefixHash` coincidan con el transcript.
4. Sólo después activar la cola del nuevo host; comandos pendientes se
   rechazan/reintentan con el mismo `commandId`.
5. Un host anterior que vuelva entra como invitado y resuelve por
   `authorityEpoch`, nunca recupera autoridad automáticamente.

Sin el checkpoint completo y el lease del relay, la arquitectura actual no
puede resolver de forma segura el caso de caída abrupta del creador. Mantener
FAIL-001 en ese caso es preferible a aceptar una divergencia silenciosa.

## Casos de fallo

1. A cae, queda B: B sólo promueve si tiene checkpoint verificable; de lo
   contrario permanece bloqueado explícitamente.
2. A cae, quedan B/C: el menor índice es candidato, pero el lease relay evita
   que ambos sean autoridad.
3. B y C detectan simultáneamente: fencing/epoch deja un único claim válido.
4. A vuelve durante la promoción: su epoch anterior queda obsoleto.
5. B promovido cae: se repite el claim sólo con epoch y checkpoint válidos.
6. Transcript atrasado: se exige prefijo coincidente y replay antes de
   aceptar comandos.
7. `trusted_command` pendiente: se reintenta con el mismo `commandId` o se
   rechaza; nunca se reasigna silenciosamente.
8. `prefixHash` distinto: no se elige ganador local; se detiene y solicita
   resync verificable.
9. Reconnect: conserva identidad/token y entra con el epoch vigente.
10. Sólo host, sin invitados: no hay sucesor; la sala queda inactiva.

## Impacto previsto

Archivos candidatos: `web/relay/src/worker.js`,
`web/ui/src/lib/relay/websocket-peer.js`,
`web/ui/src/hooks/peer-lobby/messaging.js`,
`web/ui/src/hooks/peer-lobby/trusted-sequencer.js`,
`web/ui/src/hooks/peer-lobby/crypto-resync.js` y
`web/ui/src/lib/relay/session.js`.

Mensajes nuevos probables: `host_claim`, `host_claim_result` y
`authority_state`; los mensajes `trusted_command`, `apply_action` y
`resync_request` necesitarían transportar el `authorityEpoch`. Esto confirma
que no es un cambio de una sola función.

La siguiente implementación, si se aprueba, debe empezar únicamente por el
lease/epoch del relay y sus pruebas de fencing; no por activar promoción ni
por cambiar el motor WASM.
