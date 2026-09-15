import { readFile } from "node:fs/promises";
import { createHash } from "node:crypto";
import { pathToFileURL } from "node:url";
import path from "node:path";

const base = process.env.LAB_BASE_URL || "https://lab-multiplayer.onrender.com";
const packageDir = process.env.IRONSMITH_WASM_PACKAGE
  || path.resolve(process.cwd(), "..", "ironsmith-upstream-main", "target", "npm", "ironsmith-wasm");
const pkg = await import(pathToFileURL(path.join(packageDir, "ironsmith.js")));
const bytes = name => readFile(path.join(packageDir, name));
await pkg.default({ engine: await bytes("engine_bg.wasm"), compiler: false, verifier: false });

const names = ["Smoke Plant", "Smoke Island", "Smoke Forest", "Smoke Mountain", "Smoke Swamp", "Smoke Bolt", "Smoke Bear", "Smoke Elf", "Smoke Angel", "Smoke Wizard", "Smoke Giant", "Smoke Soldier", "Smoke Aura", "Smoke Land", "Smoke Sprite"];
const cards = names.map(name => ({ identity: { name }, manaCost: "{G}", types: ["Creature"], subtypes: ["Plant"], power: "1", toughness: "1", text: "" }));
const deck = names.flatMap((_, i) => Array.from({ length: 4 }, () => cards[i]));
const config = { playerNames: ["A", "B"], startingLife: 20, seed: 42, format: "normal", decks: [{ name: "A", cards: deck }, { name: "B", cards: deck }], openingHandSize: 0 };
const hash = value => createHash("sha256").update(JSON.stringify(value)).digest("hex");
const json = async (url, options) => { const response = await fetch(`${base}${url}`, options); if (!response.ok) throw new Error(`${options?.method || "GET"} ${url}: HTTP ${response.status}`); return response.json(); };
const nextMessage = ws => new Promise((resolve, reject) => { const onMessage = event => { cleanup(); resolve(JSON.parse(typeof event.data === "string" ? event.data : Buffer.from(event.data).toString("utf8"))); }; const onError = event => { cleanup(); reject(new Error(event.message || "WebSocket error")); }; const cleanup = () => { ws.removeEventListener("message", onMessage); ws.removeEventListener("error", onError); }; ws.addEventListener("message", onMessage); ws.addEventListener("error", onError); });
async function connect(credentials, afterSeq) { const url = base.replace(/^http/, "ws") + "/v1/ws"; const ws = new WebSocket(url); await new Promise((resolve, reject) => { ws.addEventListener("open", resolve, { once: true }); ws.addEventListener("error", reject, { once: true }); }); ws.send(JSON.stringify({ type: "resume", protocol: 1, ...credentials, afterSeq, mode: "lab" })); return { ws, resumed: await nextMessage(ws) }; }
const command = { type: "priority_action", action_index: 0 };
function trusted(id, seq, actor, prefix) { return { type: "trusted_command", action: { commandId: id, seq, expectedSeq: seq, actorIndex: actor, kind: "trusted_command", command, prefixHash: prefix } }; }
const room = await json("/v1/rooms", { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ name: "WASM-A" }) });
const guest = await json(`/v1/rooms/${room.roomId}/players`, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ name: "WASM-B" }) });
const a = new pkg.WasmGame(); const b = new pkg.WasmGame();
const registration = a.registerManabrewDeckSources(config.decks);
const validation = a.validateManabrewMatchConfig(config);
a.startManabrewMatch(config); b.startManabrewMatch(config);
const aSocket = await connect(room, 0); const bSocket = await connect(guest, 0);
const transcript = []; let seq = 0; let prefix = "";
for (let index = 1; index <= 3; index += 1) {
  aSocket.ws.send(JSON.stringify(trusted(`wasm-replay-${index}`, seq, room.playerIndex, prefix)));
  const response = await nextMessage(aSocket.ws);
  if (response.type !== "apply_action") throw new Error(`LAB rejected action ${index}: ${JSON.stringify(response)}`);
  transcript.push(response.event); const applied = a.dispatch(command); if (!applied) throw new Error(`WASM A did not apply action ${index}`);
  seq = response.event.seq; prefix = response.event.prefixHash;
}
const hashA = hash(a.uiState());
for (const event of transcript) { const payload = typeof event.payload === "string" ? JSON.parse(event.payload) : event.payload; const applied = b.dispatch(payload); if (!applied) throw new Error(`WASM B did not apply seq ${event.seq}`); }
const hashB = hash(b.uiState());
if (bSocket.resumed.type !== "resumed") throw new Error(`B resume failed: ${JSON.stringify(bSocket.resumed)}`);
const beforeRecovery = await json(`/v1/rooms/${room.roomId}/diagnostics`, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ playerId: guest.playerId, resumeToken: guest.resumeToken }) });
bSocket.ws.send(JSON.stringify(trusted("wasm-replay-after-recovery", beforeRecovery.room.currentSeq, guest.playerIndex, beforeRecovery.room.recentEvents.at(-1)?.prefixHash || "")));
const action4 = await nextMessage(bSocket.ws);
console.log(JSON.stringify({ packageDir, registration, validation, labTranscript: transcript.map(event => ({ seq: event.seq, commandId: event.commandId, prefixHash: event.prefixHash })), hashA, hashB, hashEqual: hashA === hashB, continueAfterRecovery: action4.type === "apply_action", recoverySeq: action4.event?.seq, minimumData: ["match config", "seed", "card sources", "ordered accepted transcript", "sequence/prefix metadata"] }, null, 2));
aSocket.ws.close(); bSocket.ws.close();
