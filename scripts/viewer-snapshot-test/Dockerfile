FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates procps gawk && rm -rf /var/lib/apt/lists/*
# The relay (which serves the viewer it was built with) and the session's
# agent, each from a build of its own: run.sh RELAY_BIN / AGENT_BIN.
COPY relay /usr/local/bin/reminal-relay
COPY agent /usr/local/bin/reminal
COPY work.sh /work.sh
ENV REMINAL_RELAY=ws://127.0.0.1:18080/ws REMINAL_WEB=http://127.0.0.1:18080
CMD ["sleep", "infinity"]
