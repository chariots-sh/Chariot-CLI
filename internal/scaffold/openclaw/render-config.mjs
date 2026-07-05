// Renders ~/.openclaw/openclaw.json from the env Chariot injects into every
// agent container:
//   CHARIOT_PROXY_BASE_URL  OpenAI-compatible model proxy (metered credits)
//   CHARIOT_AGENT_TOKEN     bearer/API key for the proxy AND outbound replies
//   CHARIOT_MODEL           the configured model id
//
// The gateway binds 0.0.0.0:42617 so Chariot's TCP startup/liveness probes
// (which connect to the pod IP, not loopback) succeed. A non-loopback bind
// requires gateway auth; the per-agent token doubles as the gateway token —
// it never leaves the container.
import { mkdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";

const home = process.env.HOME || "/zeroclaw-data";
const proxyBaseUrl = process.env.CHARIOT_PROXY_BASE_URL;
const agentToken = process.env.CHARIOT_AGENT_TOKEN;
const model = process.env.CHARIOT_MODEL;
if (!proxyBaseUrl || !agentToken || !model) {
  console.error(
    "[chariot-openclaw] missing CHARIOT_PROXY_BASE_URL / CHARIOT_AGENT_TOKEN / CHARIOT_MODEL",
  );
  process.exit(1);
}

const config = {
  models: {
    providers: {
      chariot: {
        baseUrl: proxyBaseUrl,
        apiKey: agentToken,
        api: "openai-completions",
        // The proxy may live on a private address (host.docker.internal in
        // the local runtime); harmless when it is public.
        request: { allowPrivateNetwork: true },
        models: [
          {
            id: model,
            name: `Chariot proxy (${model})`,
            reasoning: false,
            input: ["text"],
            cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
            contextWindow: 128000,
            maxTokens: 8192,
          },
        ],
      },
    },
  },
  agents: { defaults: { model: `chariot/${model}` } },
  gateway: {
    mode: "local",
    port: 42617,
    bind: "custom",
    customBindHost: "0.0.0.0",
    auth: { mode: "token", token: agentToken },
  },
};

mkdirSync(join(home, ".openclaw"), { recursive: true });
writeFileSync(
  join(home, ".openclaw", "openclaw.json"),
  JSON.stringify(config, null, 2) + "\n",
);
console.log("[chariot-openclaw] rendered ~/.openclaw/openclaw.json");
