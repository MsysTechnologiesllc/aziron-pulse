#!/bin/bash
set -e

SETTINGS_DIR="/home/coder/.local/share/code-server/User"
SETTINGS_FILE="$SETTINGS_DIR/settings.json"
TMPL_FILE="/opt/aziron/settings.json.tmpl"

# Substitute FUSIONX_BACKEND_URL into VS Code settings.
# Default to the production Aziron Studio endpoint if the env var is unset,
# empty, or points to a localhost address (which is unreachable inside the pod).
case "${FUSIONX_BACKEND_URL}" in
    "" | http://localhost* | http://127.0.0.1*)
        FUSIONX_BACKEND_URL="https://studio.aziro.com"
        ;;
esac
export FUSIONX_BACKEND_URL
mkdir -p "$SETTINGS_DIR"
if [ -f "$TMPL_FILE" ]; then
    envsubst < "$TMPL_FILE" > "$SETTINGS_FILE"
fi

# Write VS Code global state so FusionX sidebar is the active view on first launch.
# Code-server stores workbench state in a SQLite database; we pre-seed it here so
# the user always lands on the FusionX panel, not GitHub Copilot or Explorer.
STATE_DB="$SETTINGS_DIR/globalStorage/state.vscdb"
if command -v sqlite3 >/dev/null 2>&1 && [ ! -f "$STATE_DB" ]; then
    mkdir -p "$(dirname "$STATE_DB")"
    sqlite3 "$STATE_DB" "
        CREATE TABLE IF NOT EXISTS ItemTable (key TEXT UNIQUE ON CONFLICT REPLACE, value BLOB);
        INSERT INTO ItemTable VALUES('workbench.sidebar.activeviewletid', 'fusionx-ActivityBar');
        INSERT INTO ItemTable VALUES('workbench.panel.lastactivePanel', '');
    "
fi

# Ensure the workspace directory exists and is writable by coder.
# The PVC mount point is created by kubelet as root; fix ownership before
# code-server starts so the user can create/edit files immediately.
WORKSPACE_DIR="/home/coder/workspace"
mkdir -p "$WORKSPACE_DIR"
sudo chown -R coder:coder "$WORKSPACE_DIR" 2>/dev/null || true

# Clone repository if REPO_URL is provided
if [ -n "$REPO_URL" ]; then
    WORKSPACE_DIR="/home/coder/workspace"
    REPO_NAME=$(basename "$REPO_URL" .git)
    CLONE_DIR="$WORKSPACE_DIR/$REPO_NAME"

    if [ ! -d "$CLONE_DIR/.git" ]; then
        echo "[aziron] Cloning $REPO_URL into $CLONE_DIR"

        # Configure git credentials if token is provided
        if [ -n "$GIT_ASKPASS_TOKEN" ]; then
            REPO_HOST=$(echo "$REPO_URL" | sed -E 's|https?://([^/]+)/.*|\1|')
            git config --global credential.helper store
            echo "https://oauth2:${GIT_ASKPASS_TOKEN}@${REPO_HOST}" > /home/coder/.git-credentials
            chmod 600 /home/coder/.git-credentials
        fi

        git clone --depth=1 "$REPO_URL" "$CLONE_DIR" || echo "[aziron] Warning: git clone failed, continuing without repo"
    else
        echo "[aziron] Repository already cloned at $CLONE_DIR, skipping"
    fi
fi

# Launch code-server — auth is handled by network access control (NodePort).
# --auth none disables code-server's own password prompt.
exec /usr/bin/entrypoint.sh --bind-addr 0.0.0.0:8080 --auth none /home/coder/workspace "$@"
