// One Chariot message → one OpenClaw agent turn → POST the reply to Chariot.
//
// Printing to stdout does NOT reach the user; the only reply path is
//   POST $CHARIOT_OUTBOUND_URL  with header X-Chariot-Agent-Token.
import { execFileSync } from "node:child_process";

const TURN_TIMEOUT_MS = 110_000; // Chariot expects a reply within ~2 minutes

const raw = process.argv[2] ?? "";
// Chariot flattens newlines to literal "\n" for its one-line delivery contract.
const message = raw.replaceAll("\\n", "\n").trim();
if (!message) {
  process.exit(0);
}

function extractReply(parsed) {
  // `openclaw agent --json` output carries the turn result's payloads; be
  // liberal about the envelope shape across OpenClaw versions.
  const candidates = [parsed?.payloads, parsed?.result?.payloads];
  for (const payloads of candidates) {
    if (Array.isArray(payloads)) {
      const texts = payloads.map((p) => p?.text).filter(Boolean);
      if (texts.length > 0) return texts.join("\n\n").trim();
    }
  }
  return "";
}

let reply = "";
try {
  const out = execFileSync(
    "openclaw",
    ["agent", "--session-id", "chariot-main", "--message", message, "--json"],
    { encoding: "utf8", timeout: TURN_TIMEOUT_MS, maxBuffer: 16 * 1024 * 1024 },
  );
  reply = extractReply(JSON.parse(out));
} catch (err) {
  console.error(`[chariot-openclaw] agent turn failed: ${err?.message ?? err}`);
  process.exit(1);
}
if (!reply) {
  console.error("[chariot-openclaw] agent produced no reply text; not posting");
  process.exit(1);
}

const res = await fetch(process.env.CHARIOT_OUTBOUND_URL, {
  method: "POST",
  headers: {
    "X-Chariot-Agent-Token": process.env.CHARIOT_AGENT_TOKEN,
    "Content-Type": "application/json",
  },
  body: JSON.stringify({ message: reply }),
});
if (!res.ok) {
  console.error(`[chariot-openclaw] outbound POST failed: HTTP ${res.status}`);
  process.exit(1);
}
console.log("[chariot-openclaw] reply delivered");
