# Brechas observadas en IronSmith

Fuente inspeccionada: `FiammaMuscari/ironsmith`, rama `main`, y su implementación
`web/relay` / `web/ui/src/hooks/peer-lobby`.

## Estado actual

El relay actual hace bien varias cosas: identidad persistida, chunking, colas
acotadas, heartbeat, transcript local y resync. Sin embargo, su propio README
explicita que reenvía mensajes sin guardar snapshots y que el host debe volver
para continuar. En la UI también existen barreras globales mientras un peer se
resincroniza (`resyncingPeerIdsRef`) y estados como “host actions paused”.

Ese diseño produce tres clases de congelamiento que se parecen entre sí:

1. **Transporte:** socket muerto, cola llena o chunks incompletos.
2. **Coordinación:** un ACK/resync pendiente bloquea acciones globales.
3. **Autoridad:** el navegador host no responde, se suspende o debe reconstruir.

La reconexión del socket sólo arregla la primera. Mientras la secuencia y la
recuperación definitiva vivan en el host, las otras dos permanecen.

## Cambio propuesto

Mover al coordinador estas responsabilidades:

- credencial de asiento;
- secuencia aceptada;
- deduplicación de intents;
- journal durable;
- cursor de replay;
- presencia observable, sin usarla como autoridad de juego.

Mantener inicialmente en los clientes:

- motor Rust/WASM;
- validación de reglas;
- vistas redactadas y criptografía existente;
- interfaz y catálogo de cartas.

## Experimentos de aceptación

El reemplazo sólo debería proponerse si supera de forma repetible:

- cerrar al creador en medio de una ronda y continuar con los demás;
- desconectar 30 s, 2 min y 10 min y recuperar por cursor;
- perder el broadcast posterior al commit y reintentar sin duplicar;
- enviar `expectedSeq` viejo y obtener conflicto explícito, nunca freeze;
- saturar un cliente y comprobar que sólo ese cliente cae;
- reiniciar el servidor y reconstruir exactamente la misma cadena;
- alterar una línea del journal y fallar de forma visible al cargar;
- ejecutar una partida larga y medir bytes/latencia de replay.

La siguiente etapa debe implementar un adaptador pequeño para los comandos
deterministas de IronSmith y un chaos harness que automatice esos casos.
