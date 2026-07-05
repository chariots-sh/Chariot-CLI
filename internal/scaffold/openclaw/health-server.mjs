// Chariot's readiness probe: GET /health on :8088 must return 200 once the
// agent can accept messages. This tiny server reports 200 as soon as the
// OpenClaw gateway accepts TCP connections on :42617.
import { createServer } from "node:http";
import { connect } from "node:net";

const HEALTH_PORT = 8088;
const GATEWAY_PORT = 42617;

function gatewayUp() {
  return new Promise((resolve) => {
    const sock = connect({ port: GATEWAY_PORT, host: "127.0.0.1" }, () => {
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

createServer(async (req, res) => {
  if (req.url === "/health" || req.url === "/health/") {
    const up = await gatewayUp();
    res.writeHead(up ? 200 : 503, { "content-type": "text/plain" });
    res.end(up ? "ok" : "starting");
    return;
  }
  res.writeHead(404, { "content-type": "text/plain" });
  res.end("not found");
}).listen(HEALTH_PORT, "0.0.0.0", () => {
  console.log(`[chariot-openclaw] health server on :${HEALTH_PORT}`);
});
