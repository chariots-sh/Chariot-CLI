# syntax=docker/dockerfile:1
# Chariot custom agent image — OpenClaw runtime.
#
#   docker build -t my-openclaw-agent .
#   chariot image push my-openclaw-agent:latest --pod-size medium
#
# Pin a specific OpenClaw release with:
#   docker build --build-arg OPENCLAW_VERSION=2026.6.11 -t my-openclaw-agent .
FROM node:24-bookworm-slim

ARG OPENCLAW_VERSION=latest
RUN npm install -g openclaw@${OPENCLAW_VERSION} && npm cache clean --force

# The Chariot contract shim + boot scripts (see README.md in this directory).
COPY zeroclaw /usr/local/bin/zeroclaw
COPY entrypoint.sh render-config.mjs health-server.mjs turn.mjs /chariot/
RUN chmod 755 /usr/local/bin/zeroclaw /chariot/entrypoint.sh

# Chariot enforces UID:GID 65534:65534 and a read-only root filesystem at
# runtime; /zeroclaw-data (HOME) is the only writable path. In the fleet a
# persistent volume is mounted there; baking the tree into the image too makes
# it work anywhere (plain `docker run`, no volume needed).
RUN mkdir -p /zeroclaw-data/workspace /zeroclaw-data/.openclaw /zeroclaw-data/tmp \
    && chown -R 65534:65534 /zeroclaw-data
USER 65534:65534
ENV HOME=/zeroclaw-data

# Local-docker readiness analog of Chariot's Kubernetes /health probe
# (Kubernetes ignores HEALTHCHECK and runs its own probes on the same URL).
HEALTHCHECK --interval=5s --timeout=3s --start-period=10s --retries=60 \
  CMD ["node", "-e", "fetch('http://127.0.0.1:8088/health').then(r=>process.exit(r.ok?0:1)).catch(()=>process.exit(1))"]

ENTRYPOINT ["/chariot/entrypoint.sh"]
