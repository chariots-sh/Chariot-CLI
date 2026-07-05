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

# Readiness endpoint on :8088/health (Chariot's readiness probe) — reports 200
# once the OpenClaw gateway accepts connections on :42617.
node /chariot/health-server.mjs &

# The gateway binds 0.0.0.0:42617 (Chariot's startup/liveness probe port) —
# configured in the rendered openclaw.json.
exec openclaw gateway
