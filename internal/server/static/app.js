const $ = selector => document.querySelector(selector);
const storageKey = "ironsmith-multiplayer-lab-session-v1";
let credentials = readCredentials();
let socket;
let sequence = 0;
let serverSequence = 0;
let prefixHash = "";
let reconnects = 0;
let metrics = { duplicates: 0, conflicts: 0, divergences: 0 };
let lastCommand = null;
let reconnectAttempt = 0;
let reconnectTimer;
let manualClose = false;

function readCredentials() {
  try { return JSON.parse(localStorage.getItem(storageKey)); } catch { return null; }
}

function saveCredentials(value) {
  credentials = { ...value, mode: $("#mode").value };
  localStorage.setItem(storageKey, JSON.stringify(value));
  renderIdentity();
}

function replaceCredentials(value) {
  manualClose = true;
  clearTimeout(reconnectTimer);
  socket?.close(1000, "switch session");
  socket = null;
  updateSequence(0);
  saveCredentials(value);
}

function renderIdentity() {
  $("#room").textContent = credentials?.roomId || "—";
  $("#player").textContent = credentials?.playerId || "—";
  $("#actor").textContent = credentials?.playerIndex ?? "—";
  $("#join-form [name=room]").value = credentials?.roomId || "";
  $("#mode").value = credentials?.mode || "lab";
}

function setStatus(text, kind = "offline") {
  $("#status").textContent = text;
  $("#status-dot").className = `dot ${kind}`;
  $("#action").disabled = kind !== "online";
  $("#burst").disabled = kind !== "online";
  $("#duplicate").disabled = kind !== "online" || !lastCommand;
  $("#disconnect").disabled = !socket || socket.readyState > WebSocket.OPEN;
  $("#reconnect").disabled = !credentials || kind === "online" || kind === "connecting";
  $("#diagnostics").disabled = !credentials;
}

function log(text, error = false) {
  const item = document.createElement("li");
  item.textContent = `${new Date().toLocaleTimeString()}  ${text}`;
  if (error) item.className = "error";
  $("#log").prepend(item);
}

function updateSequence(value) {
  sequence = Number(value);
  $("#sequence").textContent = `${sequence} / ${serverSequence}`;
}

function renderMetrics() {
  $("#prefix").textContent = prefixHash ? prefixHash.slice(0, 16) + "…" : "—";
  $("#reconnects").textContent = reconnects;
  $("#metrics").textContent = `${metrics.duplicates} / ${metrics.conflicts} / ${metrics.divergences}`;
}

async function post(url, body) {
  const response = await fetch(url, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify(body) });
  const value = await response.json();
  if (!response.ok) throw new Error(value.message || `HTTP ${response.status}`);
  return value;
}

function connect() {
  clearTimeout(reconnectTimer);
  if (!credentials || socket?.readyState === WebSocket.OPEN || socket?.readyState === WebSocket.CONNECTING) return;
  manualClose = false;
  setStatus(reconnectAttempt ? `Reconectando (intento ${reconnectAttempt + 1})` : "Conectando", "connecting");
  const scheme = location.protocol === "https:" ? "wss:" : "ws:";
  const ws = new WebSocket(`${scheme}//${location.host}/v1/ws`);
  socket = ws;
  ws.onopen = () => ws.send(JSON.stringify({ type: "resume", protocol: 1, ...credentials, afterSeq: sequence, mode: credentials.mode || "lab" }));
  ws.onmessage = ({ data }) => {
    const message = JSON.parse(data);
    if (message.type === "error") { log(`${message.code}: ${message.message}`, true); return; }
    if (message.type === "resumed") {
      serverSequence = Number(message.currentSeq || sequence); renderMetrics();
      for (const event of message.events || []) applyEvent(event);
      reconnectAttempt = 0;
      setStatus(`Conectado · ${message.mode || credentials.mode || "lab"}`, "online");
      log(`sesión reanudada; ${message.events?.length || 0} eventos recuperados`);
      return;
    }
    if ((message.type === "event" || message.type === "apply_action") && message.event) applyEvent(message.event);
    if (message.type === "trusted_command_ack" && message.duplicate) { metrics.duplicates++; renderMetrics(); log(`duplicate commandId confirmado en seq ${message.currentSeq}`); }
    if (message.type === "state_resync") {
      metrics.divergences += message.divergence ? 1 : 0; serverSequence = Number(message.currentSeq || 0);
      sequence = 0; prefixHash = ""; for (const event of message.events || []) applyEvent(event);
      renderMetrics(); log("state_resync aplicado por divergencia de prefixHash", true);
    }
    if (message.type === "error") {
      if (message.code === "sequence_conflict") metrics.conflicts++;
      renderMetrics();
    }
  };
  ws.onclose = () => {
    if (socket !== ws) return;
    socket = null;
    if (!manualClose) reconnects++;
    renderMetrics(); setStatus(manualClose ? "Desconectado manualmente" : "Conexión perdida", "offline");
    if (!manualClose) scheduleReconnect();
  };
  ws.onerror = () => log("error de transporte", true);
}

function applyEvent(event) {
  if (event.seq <= sequence) return;
  if (event.seq !== sequence + 1) {
    log(`gap detectado: esperaba ${sequence + 1}, llegó ${event.seq}; resincronizando`, true);
    socket?.close(4000, "sequence gap");
    return;
  }
  updateSequence(event.seq);
  serverSequence = Math.max(serverSequence, Number(event.seq)); prefixHash = event.prefixHash || prefixHash; renderMetrics();
  log(`#${event.seq} apply_action ${event.commandId || event.actionId} · actor ${event.actorIndex}`);
}

function scheduleReconnect() {
  reconnectAttempt++;
  const base = Math.min(10000, 250 * 2 ** Math.min(reconnectAttempt, 6));
  const delay = Math.round(base * (.75 + Math.random() * .5));
  reconnectTimer = setTimeout(connect, delay);
  setStatus(`Reconexión en ${(delay / 1000).toFixed(1)} s`, "connecting");
}

$("#create-form").addEventListener("submit", async event => {
  event.preventDefault();
  try {
    replaceCredentials(await post("/v1/rooms", { name: new FormData(event.currentTarget).get("name") }));
    log(`sala creada: ${credentials.roomId}`);
    connect();
  } catch (error) { log(error.message, true); }
});

$("#mode").addEventListener("change", () => {
  if (credentials) { credentials.mode = $("#mode").value; localStorage.setItem(storageKey, JSON.stringify(credentials)); }
});

$("#join-form").addEventListener("submit", async event => {
  event.preventDefault();
  const form = new FormData(event.currentTarget);
  try {
    replaceCredentials(await post(`/v1/rooms/${encodeURIComponent(form.get("room"))}/players`, { name: form.get("name") }));
    log(`asiento creado: ${credentials.playerId}`);
    connect();
  } catch (error) { log(error.message, true); }
});

function sendCommand(command = null) {
  if (socket?.readyState !== WebSocket.OPEN) return;
  const value = command || { type: "priority_action", action_ref: { kind: "pass_priority" }, clientTime: new Date().toISOString() };
  const commandId = command ? lastCommand.commandId : `command-${crypto.randomUUID()}`;
  lastCommand = { commandId, command: value };
  socket.send(JSON.stringify({ type: "trusted_command", action: { commandId, seq: sequence, expectedSeq: sequence, actorIndex: credentials.playerIndex, kind: "trusted_command", command: value, prefixHash } }));
  log(`trusted_command ${commandId} desde seq ${sequence}`); renderMetrics();
}
$("#action").addEventListener("click", () => sendCommand());
$("#burst").addEventListener("click", async () => { for (let i = 0; i < 10; i++) { sendCommand(); await new Promise(r => setTimeout(r, 10)); } });
$("#duplicate").addEventListener("click", () => sendCommand(lastCommand?.command));

$("#disconnect").addEventListener("click", () => { manualClose = true; clearTimeout(reconnectTimer); socket?.close(1000, "manual test"); });
$("#reconnect").addEventListener("click", () => { reconnectAttempt = 0; connect(); });
$("#clear").addEventListener("click", () => { $("#log").replaceChildren(); });
$("#diagnostics").addEventListener("click", async () => {
  try {
    const server = await post(`/v1/rooms/${encodeURIComponent(credentials.roomId)}/diagnostics`, {
      playerId: credentials.playerId,
      resumeToken: credentials.resumeToken
    });
    const bundle = {
      generatedAt: new Date().toISOString(),
      client: {
        roomId: credentials.roomId,
        playerId: credentials.playerId,
        playerIndex: credentials.playerIndex,
        localSequence: sequence,
        socketState: socket?.readyState ?? WebSocket.CLOSED,
        reconnectAttempt,
        online: navigator.onLine,
        visibility: document.visibilityState
      },
      server
    };
    const blob = new Blob([JSON.stringify(bundle, null, 2)], { type: "application/json" });
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = url;
    link.download = `ironsmith-multiplayer-diagnostic-${credentials.roomId}-${Date.now()}.json`;
    link.click();
    URL.revokeObjectURL(url);
    log("diagnóstico descargado (sin token ni payloads de juego)");
  } catch (error) { log(`diagnóstico: ${error.message}`, true); }
});

renderIdentity();
updateSequence(0);
renderMetrics();
if (credentials) connect();
