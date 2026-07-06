// The Chariot agent-gateway endpoint this image must serve on :8088:
//
//   GET  /health   → 200 once the agent can accept messages (readiness probe;
//                    startup/liveness probes only need the socket open)
//   POST /message  → {"message": ..., "message_id": ...} with header
//                    X-Gateway-Token == $AGENT_GATEWAY_TOKEN. Respond 2xx once
//                    the message is safely accepted; the reply is delivered
//                    asynchronously by POSTing to $CHARIOT_OUTBOUND_URL
//                    (see turn.mjs). message_id is a dedupe key — Chariot
//                    retries failed deliveries with the SAME id.
import { execFile } from "node:child_process";
import { createServer } from "node:http";
import { connect } from "node:net";

const GATEWAY_PORT = 8088;
const OPENCLAW_PORT = 42617; // the OpenClaw gateway, internal to this image

// Remember recently accepted message ids so a redelivery never runs twice.
const MAX_SEEN = 1000;
const seen = new Set();

function openclawUp() {
  return new Promise((resolve) => {
    const sock = connect({ port: OPENCLAW_PORT, host: "127.0.0.1" }, () => {
      sock.destroy();
      resolve(true);
    });
    sock.on("error", () => resolve(false));
    sock.setTimeout(1000, () => {
      sock.destroy();
      resolve(false);
    });
  });
}

function runTurn(message) {
  // One OpenClaw agent turn per message; turn.mjs POSTs the reply to Chariot.
  execFile(
    "node",
    ["/chariot/turn.mjs", message],
    { timeout: 150_000 },
    (err, stdout, stderr) => {
      if (err) console.error(`[chariot-openclaw] turn failed: ${err.message}\n${stderr}`);
      else if (stdout.trim()) console.log(stdout.trim());
    },
  );
}

function readBody(req) {
  return new Promise((resolve, reject) => {
    const chunks = [];
    req.on("data", (c) => chunks.push(c));
    req.on("end", () => resolve(Buffer.concat(chunks).toString("utf8")));
    req.on("error", reject);
  });
}

createServer(async (req, res) => {
  const respond = (code, body) => {
    res.writeHead(code, { "content-type": "application/json" });
    res.end(JSON.stringify(body));
  };
  if (req.method === "GET" && (req.url === "/health" || req.url === "/health/")) {
    const up = await openclawUp();
    return respond(up ? 200 : 503, { status: up ? "ok" : "starting" });
  }
  if (req.method === "POST" && req.url === "/message") {
    const expected = process.env.AGENT_GATEWAY_TOKEN || "";
    if (!expected || req.headers["x-gateway-token"] !== expected) {
      return respond(401, { error: "bad gateway token" });
    }
    let parsed;
    try {
      parsed = JSON.parse(await readBody(req));
    } catch {
      return respond(400, { error: "invalid JSON" });
    }
    const message = typeof parsed?.message === "string" ? parsed.message : "";
    if (!message.trim()) {
      return respond(400, { error: "missing message" });
    }
    const id = typeof parsed?.message_id === "string" ? parsed.message_id : "";
    if (id) {
      if (seen.has(id)) {
        return respond(202, { status: "duplicate", message_id: id }); // already accepted
      }
      seen.add(id);
      if (seen.size > MAX_SEEN) {
        seen.delete(seen.values().next().value); // drop the oldest
      }
    }
    respond(202, { status: "accepted", message_id: id });
    runTurn(message); // async — the reply goes out via the outbound POST
    return;
  }
  respond(404, { error: "not found" });
}).listen(GATEWAY_PORT, "0.0.0.0", () => {
  console.log(`[chariot-openclaw] agent gateway on :${GATEWAY_PORT}`);
});
