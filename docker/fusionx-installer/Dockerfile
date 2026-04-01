FROM codercom/code-server:latest

USER root

# Install envsubst (gettext-base), git, and sqlite3
RUN apt-get update && apt-get install -y --no-install-recommends \
    gettext-base \
    git \
    sqlite3 \
    && rm -rf /var/lib/apt/lists/*

# Copy FusionX VSIX
COPY fusionx.vsix /opt/fusionx/fusionx.vsix

# Install FusionX extension for the coder user
RUN code-server \
    --install-extension /opt/fusionx/fusionx.vsix \
    --user-data-dir /home/coder/.local/share/code-server \
    && chown -R coder:coder /home/coder/.local

# Pre-create VS Code user settings directory
RUN mkdir -p /home/coder/.local/share/code-server/User && \
    chown -R coder:coder /home/coder/.local/share/code-server/User

# Copy settings template and startup script
COPY settings.json.tmpl /opt/aziron/settings.json.tmpl
COPY startup.sh /opt/aziron/startup.sh
RUN chmod +x /opt/aziron/startup.sh && \
    chown coder:coder /opt/aziron/startup.sh

USER coder

# Override the entrypoint to run our startup script
ENTRYPOINT ["/opt/aziron/startup.sh"]
