// One Chariot message → one OpenClaw agent turn → POST the reply to Chariot.
// Invoked by gateway-server.mjs for each accepted POST /message.
//
// Printing to stdout does NOT reach the user; the only reply path is
//   POST $CHARIOT_OUTBOUND_URL  with header X-Chariot-Agent-Token.
import { execFileSync } from "node:child_process";

const TURN_TIMEOUT_MS = 110_000; // Chariot expects a reply within ~2 minutes

const message = (process.argv[2] ?? "").trim();
if (!message) {
  process.exit(0);
}

// The message is passed to the agent VERBATIM — no framing. Instructing the
// model about delivery mechanics here backfires: Chariot's own verification
// probe says "printing text does not reach the user, run this command", and a
// frame contradicting that ("your text is delivered automatically") cornered
// gpt-4o-mini into producing NO text at all on many turns. Unframed, the
// model may narrate a failed reply-command attempt, but it reliably produces
// text — and this runtime delivers that text. Tune reply style via your own
// OpenClaw system prompt if needed.

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

function runTurn(text) {
  const out = execFileSync(
    "openclaw",
    ["agent", "--session-id", "chariot-main", "--message", text, "--json"],
    { encoding: "utf8", timeout: TURN_TIMEOUT_MS, maxBuffer: 16 * 1024 * 1024 },
  );
  return extractReply(JSON.parse(out));
}

let reply = "";
try {
  reply = runTurn(message);
  if (!reply) {
    // A model occasionally ends a turn with no text (e.g. it believes it
    // already delivered the reply some other way). One explicit nudge.
    console.error("[chariot-openclaw] empty turn; retrying with a nudge");
    reply = runTurn(
      "Your previous turn produced no text. Write your reply to the last " +
        "message now, as plain text — it is delivered to the user automatically.",
    );
  }
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
