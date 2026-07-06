#!/bin/sh
# Chariot starts the container with args ["daemon"]. Anything else is a
# passthrough for local debugging (e.g. `docker run -it my-image sh`).
set -eu

if [ "${1:-}" != "daemon" ]; then
  exec "$@"
fi

export HOME="${HOME:-/zeroclaw-data}"
# The root filesystem is read-only in the fleet; keep every writable path
# (including Node's os.tmpdir) under HOME.
export TMPDIR="$HOME/tmp"
mkdir -p "$TMPDIR" "$HOME/.openclaw" "$HOME/workspace"

# Render ~/.openclaw/openclaw.json from the CHARIOT_* env Chariot injects.
node /chariot/render-config.mjs

# The Chariot agent-gateway endpoint on :8088 — Chariot's probes (/health) and
# message delivery (POST /message) all land here; it runs each accepted
# message as an OpenClaw agent turn (turn.mjs) and POSTs the reply back.
node /chariot/gateway-server.mjs &

# The OpenClaw gateway itself (loopback :42617, internal to this image).
exec openclaw gateway
