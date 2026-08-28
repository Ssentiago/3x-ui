#!/usr/bin/env bash
set -euo pipefail

HOST="root@185.43.5.31"
SSH_PASSWORD="${XUI_SSH_PASSWORD:?переменная XUI_SSH_PASSWORD не задана}"
PROJECT_DIR="/Users/itego/IdeaProjects/3x-ui"
FRONTEND_DIR="$PROJECT_DIR/frontend"
LOCAL_FILE="$PROJECT_DIR/x-ui-linux"
REMOTE_TMP="/tmp/x-ui"
REMOTE_PATH="/usr/local/x-ui/x-ui"
CTRL_SOCK="/tmp/ssh-ctrl-%r@%h:%p"

SSH_OPTS=(-o ControlMaster=auto -o ControlPath="$CTRL_SOCK" -o ControlPersist=60)

( cd "$FRONTEND_DIR" && npm run build )

( cd "$PROJECT_DIR" && \
  CC=x86_64-unknown-linux-musl-gcc CGO_ENABLED=1 GOOS=linux GOARCH=amd64 \
  go build -ldflags '-linkmode external -extldflags "-static"' -o x-ui-linux )

sshpass -p "$SSH_PASSWORD" scp "${SSH_OPTS[@]}" "$LOCAL_FILE" "$HOST:$REMOTE_TMP"

sshpass -p "$SSH_PASSWORD" ssh "${SSH_OPTS[@]}" "$HOST" \
  "systemctl stop x-ui && mv $REMOTE_TMP $REMOTE_PATH && chmod +x $REMOTE_PATH && systemctl start x-ui"