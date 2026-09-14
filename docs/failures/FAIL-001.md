# FAIL-001

Title: creator disconnect blocks room progress in baseline profile

Mode: BASELINE

Initial: A created a room, B joined, and both applied one command each. Both
clients reached server seq 2 with matching prefix metadata.

Steps:

1. Connect A and B using the public Render service.
2. Apply commands from A and B.
3. Disconnect A and wait until the server reports only B active.
4. Send the next trusted command from B.
5. Reconnect A from cursor 2.

Expected: record the creator-loss behavior without conflating it with a
transport failure; compare the same sequence in LAB.

Actual BASELINE: B receives `host_unavailable`; server remains at seq 2. A
reconnects successfully and has zero missing events.

Reproduction: 3/3 valid runs. Additional confirmation: 1 run after harness
measurement correction.

LAB result: the same scenario accepts B's command as seq 3; A reconnects and
replays exactly one event. Reproduced 3/3, with one additional confirmation.

Evidence: Render rooms used were baseline `room-851d5d7279a3`,
`room-9252880a1800`, `room-69729be8bc11`; LAB rooms were
`room-ccee7ec6f5cc`, `room-c186e21b1a7e`, `room-bba93e4409f3`. No credentials
or resume tokens are stored here.

Related reference behavior: `web/ui/src/lib/relay/websocket-peer.js` handles
peer lifecycle and host advertisement; `web/ui/src/lib/relay/resync.js`
defines cursor/prefix recovery. This evidence does not establish root cause.
