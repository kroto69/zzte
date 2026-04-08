#!/usr/bin/env bash
set -euo pipefail

APP_DIR="/opt/olt-monit"
APP_USER="oltmonit"
SERVICE_NAME="olt-monit"
BIN_PATH="${APP_DIR}/olt-monit"
CONFIG_PATH="${APP_DIR}/config/olt_config.yaml"

if [ "$(id -u)" -ne 0 ]; then
  echo "Please run as root (sudo)."
  exit 1
fi

echo "==> Installing dependencies (redis-server)"
apt-get update -y
apt-get install -y redis-server
systemctl enable --now redis-server

if ! id -u "${APP_USER}" >/dev/null 2>&1; then
  echo "==> Creating user ${APP_USER}"
  useradd -r -s /bin/false "${APP_USER}"
fi

if [ ! -d "${APP_DIR}" ]; then
  echo "==> Creating ${APP_DIR}"
  mkdir -p "${APP_DIR}"
fi

echo "==> Setting ownership"
chown -R "${APP_USER}:${APP_USER}" "${APP_DIR}"

if [ ! -f "${BIN_PATH}" ]; then
  echo "WARNING: ${BIN_PATH} not found."
  echo "Copy the binary (named: olt-monit) and project files to ${APP_DIR} before starting the service."
fi

if [ ! -f "${CONFIG_PATH}" ]; then
  echo "WARNING: ${CONFIG_PATH} not found."
  echo "Copy your config folder to ${APP_DIR}/config."
fi

echo "==> Writing systemd service"
cat > "/etc/systemd/system/${SERVICE_NAME}.service" <<EOF
[Unit]
Description=OLT Monitor Service
After=network.target redis-server.service
Wants=redis-server.service

[Service]
Type=simple
User=${APP_USER}
WorkingDirectory=${APP_DIR}
ExecStart=${BIN_PATH}
Restart=always
RestartSec=3
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
EOF

echo "==> Enabling service"
systemctl daemon-reload
systemctl enable --now "${SERVICE_NAME}"

echo "==> Status"
systemctl status "${SERVICE_NAME}" --no-pager
echo "Done."
