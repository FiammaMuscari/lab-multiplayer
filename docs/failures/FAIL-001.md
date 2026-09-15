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

## Root cause evidence

The fork's relay match uses the creating peer as the host authority. This is
the mechanism demonstrated by the source inspection:

- `web/ui/src/hooks/peer-lobby/trusted-sequencer.js`,
  `acceptTrustedCommand`: only a session whose `role` is `host` allocates the
  next sequence and applies a trusted command. A client submits through
  `hostConnectionRef.current`; a missing connection cannot reach this path.
- `web/ui/src/hooks/peer-lobby/messaging.js`, `handleHostConnectionLost`:
  heartbeat loss clears `hostConnectionRef.current`, marks the host player
  disconnected, then attempts host promotion or schedules a reconnect.
- The same file, `promoteLocalPlayerToHost`: immediately returns `false` for a
  relay lobby (`isRelayId(lobbyId)`). Therefore a relay guest is not elected as
  a replacement authority when the original host disappears.
- `web/ui/src/hooks/peer-lobby/messaging.js`, `requestResync`: a client sends
  `resync_request` only when `hostConnectionRef.current` exists and is open.
  After host loss that precondition is false, so recovery cannot start through
  the lost host connection.
- `web/ui/src/lib/relay/websocket-peer.js`: an `offline` relay event closes
  connections whose peer is the offline id; a WebSocket close also closes all
  logical connections and marks the peer disconnected. `RelayConnection.send`
  rejects while closed.

The causal chain is therefore: the host peer disappears; relay lifecycle
closes the guest's logical host connection; the guest retains its session but
has no open authority connection; relay sessions explicitly skip host
promotion; trusted command acceptance and host-directed resync have no
alternate endpoint. `host_unavailable` is the resulting symptom, not the
cause. This explains the observed sequence remaining at 2 without asserting
that the lab's baseline policy is a complete port of the game engine.

## Experimental mitigation

### Design

**Current:** LAB stores the room journal, authoritative sequence, command ID
index, and prefix chain in the server-side room. Player sockets only register
active connections and subscriptions. BASELINE adds the creator-online gate
solely as a comparison control.

**Change:** no production behavior change is required. The missing piece was a
single regression that runs the same creator-disconnect scenario through both
profiles and checks guest progress plus creator replay.

**Why it addresses FAIL-001:** LAB's authority is the room journal, not the
creator socket. After the creator is closed, the guest can allocate the next
server sequence; reopening the creator's identity replays the missing suffix.

**What it does not solve:** this does not implement host migration,
distributed elections, or a port of IronSmith's game rules. It only validates
the lab's minimal authority/socket separation.

**Risks and invariants:** one room lock still serializes sequence assignment;
sequence values remain monotonic; command IDs remain idempotent; clients cannot
choose the authoritative sequence; player identity uses the resume credential;
creator loss leaves the room journal intact; and BASELINE retains its failure.

### Regression evidence

- **AUTOMATED REGRESSION: PASS** (`baseline` and `lab` subtests).
- BASELINE: creator closes after sequence 2; guest receives
  `ErrHostUnavailable`; sequence remains 2.
- LAB: creator closes after sequence 2; guest commits sequence 3; creator
  reopens and replays exactly one event with the same prefix hash.
