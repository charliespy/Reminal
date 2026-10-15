# A real code-server with the extension installed and reminal where the
# installer puts it (~/.local/bin, which is not on code-server's PATH, so the
# extension's own lookup is what finds it). The relay runs in the same
# container, so nothing reaches the hosted relay.
ARG CODE_SERVER=4.96.4
FROM codercom/code-server:${CODE_SERVER}
USER root
RUN apt-get update && apt-get install -y --no-install-recommends procps && rm -rf /var/lib/apt/lists/*
USER coder
ENV REMINAL_RELAY=ws://127.0.0.1:18080/ws REMINAL_WEB=http://127.0.0.1:18080 SHELL=/bin/bash
COPY --chown=coder:coder reminal /home/coder/.local/bin/reminal
COPY --chown=coder:coder reminal.vsix /tmp/reminal.vsix
COPY fake-agent /usr/local/bin/fake-agent
# The workspace below names this as reminal.path. Only the user's settings may
# choose the program, so it must never run: it leaves a mark if it does.
COPY ws-reminal /opt/ws-reminal
COPY --chown=coder:coder settings.json /home/coder/.local/share/code-server/User/settings.json
RUN code-server --install-extension /tmp/reminal.vsix \
 && mkdir -p /home/coder/demo-project/src \
 && printf '# demo-project\n\nDummy content for the reminal VS Code extension test.\n' > /home/coder/demo-project/README.md \
 && printf 'console.log("hello");\n' > /home/coder/demo-project/src/index.js \
 && mkdir -p '/home/coder/demo-project/[docs](command:workbench.action.showCommands)' \
 && mkdir -p /home/coder/demo-project/.vscode \
 && printf '{ "reminal.path": "/opt/ws-reminal" }\n' > /home/coder/demo-project/.vscode/settings.json
WORKDIR /home/coder/demo-project
CMD ["code-server", "--auth", "none", "--bind-addr", "127.0.0.1:8443", "--disable-telemetry", "--disable-update-check", "--disable-workspace-trust", "/home/coder/demo-project"]
