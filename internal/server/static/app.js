const $ = selector => document.querySelector(selector);
const storageKey = "ironsmith-multiplayer-lab-session-v1";
let credentials = readCredentials();
let socket;
let sequence = 0;
let reconnectAttempt = 0;
let reconnectTimer;
let manualClose = false;

function readCredentials() {
  try { return JSON.parse(localStorage.getItem(storageKey)); } catch { return null; }
}

function saveCredentials(value) {
  credentials = value;
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
  $("#join-form [name=room]").value = credentials?.roomId || "";
}

function setStatus(text, kind = "offline") {
  $("#status").textContent = text;
  $("#status-dot").className = `dot ${kind}`;
  $("#action").disabled = kind !== "online";
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
  $("#sequence").textContent = `secuencia ${sequence}`;
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
  ws.onopen = () => ws.send(JSON.stringify({ type: "resume", protocol: 1, ...credentials, afterSeq: sequence }));
  ws.onmessage = ({ data }) => {
    const message = JSON.parse(data);
    if (message.type === "error") { log(`${message.code}: ${message.message}`, true); return; }
    if (message.type === "resumed") {
      for (const event of message.events || []) applyEvent(event);
      reconnectAttempt = 0;
      setStatus("Conectado", "online");
      log(`sesión reanudada; ${message.events?.length || 0} eventos recuperados`);
      return;
    }
    if (message.type === "event" && message.event) applyEvent(message.event);
    if (message.type === "action_ack" && message.duplicate) log(`retry confirmado en seq ${message.currentSeq}`);
  };
  ws.onclose = () => {
    if (socket !== ws) return;
    socket = null;
    setStatus(manualClose ? "Desconectado manualmente" : "Conexión perdida", "offline");
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
  log(`#${event.seq} ${event.kind} · ${event.playerId}`);
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

$("#join-form").addEventListener("submit", async event => {
  event.preventDefault();
  const form = new FormData(event.currentTarget);
  try {
    replaceCredentials(await post(`/v1/rooms/${encodeURIComponent(form.get("room"))}/players`, { name: form.get("name") }));
    log(`asiento creado: ${credentials.playerId}`);
    connect();
  } catch (error) { log(error.message, true); }
});

$("#action").addEventListener("click", () => {
  if (socket?.readyState !== WebSocket.OPEN) return;
  const actionId = `action-${crypto.randomUUID()}`;
  socket.send(JSON.stringify({ type: "action", action: { actionId, expectedSeq: sequence, actorIndex: credentials.playerIndex, kind: "trusted_command", payload: { type: "priority_action", action_ref: { kind: "pass_priority" }, clientTime: new Date().toISOString() } } }));
  log(`intent enviado desde seq ${sequence}`);
});

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
if (credentials) connect();
