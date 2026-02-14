#!/bin/bash
#
# install-relay.sh — Install huzaa-relay on a Debian VPS (IONOS or similar).
# Run as root on the same host as Ergo, after setup-irc-vps.sh (or after you have
# Go and Let's Encrypt certs for RELAY_DOMAIN).
#
# Prereqs: DNS for RELAY_DOMAIN points here; ports 5349 (relay TLS) and dcc_port
# range (e.g. 50000–50100) reachable. Port 80 used only for initial cert if needed.
#
# Default RELAY_DOMAIN is irc.example.com; set RELAY_DOMAIN only if you use a different host.
#
if grep -q $'\r' "$0" 2>/dev/null; then
  exec sed 's/\r$//' "$0" | bash -s "$@"
fi
set -e
export DEBIAN_FRONTEND=noninteractive

RELAY_DOMAIN="${RELAY_DOMAIN:-irc.example.com}"
RELAY_HOME="/opt/huzaa-relay"
RELAY_USER="huzaa-relay"
CERT_DIR="/etc/letsencrypt/live/${RELAY_DOMAIN}"
RELAY_CERTS="${RELAY_HOME}/certs"
GO_VERSION="1.23.5"

echo "=== 1. Go (if not already installed) ==="
if ! command -v go &>/dev/null; then
  apt-get update -qq
  apt-get install -y -qq wget
  cd /tmp
  wget -q "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz"
  rm -rf /usr/local/go
  tar -C /usr/local -xzf "go${GO_VERSION}.linux-amd64.tar.gz"
  rm -f "go${GO_VERSION}.linux-amd64.tar.gz"
  echo 'export PATH=$PATH:/usr/local/go/bin' > /etc/profile.d/golang.sh
  export PATH="$PATH:/usr/local/go/bin"
fi
export PATH="$PATH:/usr/local/go/bin"

echo "=== 2. Fetch huzaa-relay and build ==="
GOPATH="${GOPATH:-/root/go}"
export GOPATH
mkdir -p "$GOPATH/src/github.com/awgh"
if [[ ! -d "$GOPATH/src/github.com/awgh/huzaa-relay/.git" ]]; then
  apt-get install -y -qq git
  git clone --depth 1 https://github.com/awgh/huzaa-relay.git "$GOPATH/src/github.com/awgh/huzaa-relay"
fi
cd "$GOPATH/src/github.com/awgh/huzaa-relay"
go build -o relay_bin ./cmd/relay

echo "=== 3. huzaa-relay user and directories ==="
if ! id "$RELAY_USER" &>/dev/null; then
  useradd --system --home-dir "$RELAY_HOME" --no-create-home --shell /usr/sbin/nologin "$RELAY_USER"
fi
mkdir -p "$RELAY_HOME" "$RELAY_CERTS"
cp -f relay_bin "$RELAY_HOME/relay"

echo "=== 4. TLS certs (copy from Let's Encrypt) ==="
if [[ -d "$CERT_DIR" ]]; then
  cp -L "${CERT_DIR}/fullchain.pem" "${RELAY_CERTS}/fullchain.pem"
  cp -L "${CERT_DIR}/privkey.pem" "${RELAY_CERTS}/privkey.pem"
  chmod 600 "${RELAY_CERTS}/privkey.pem"
else
  echo "Warning: $CERT_DIR not found. Create a cert first (e.g. run setup-irc-vps.sh or certbot)."
  echo "Then copy certs to ${RELAY_CERTS}/ and restart: systemctl restart huzaa-relay"
fi
chown -R "$RELAY_USER:$RELAY_USER" "$RELAY_HOME"

echo "=== 5. Relay config ==="
cat > "$RELAY_HOME/relay.json" << EOF
{
  "turn_listen": ":5349",
  "turn_users": [
    { "username": "huzaa-bot", "secret": "REPLACE_WITH_SECRET" }
  ],
  "dcc_port_min": 50000,
  "dcc_port_max": 50100,
  "relay_host": "${RELAY_DOMAIN}",
  "tls_cert_file": "${RELAY_CERTS}/fullchain.pem",
  "tls_key_file": "${RELAY_CERTS}/privkey.pem",
  "max_sessions": 100
}
EOF
chown "$RELAY_USER:$RELAY_USER" "$RELAY_HOME/relay.json"

echo "=== 6. systemd service ==="
cat > /etc/systemd/system/huzaa-relay.service << EOF
[Unit]
Description=Huzaa DCC relay for IRC file-sharing
After=network.target

[Service]
Type=simple
User=${RELAY_USER}
Group=${RELAY_USER}
WorkingDirectory=${RELAY_HOME}
ExecStart=${RELAY_HOME}/relay -config ${RELAY_HOME}/relay.json
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

echo "=== 7. Certbot deploy hook (reload relay on cert renewal) ==="
mkdir -p /etc/letsencrypt/renewal-hooks/deploy
cat > /etc/letsencrypt/renewal-hooks/deploy/huzaa-relay-reload.sh << DEPLOY
#!/bin/bash
cp -L ${CERT_DIR}/fullchain.pem ${RELAY_CERTS}/fullchain.pem
cp -L ${CERT_DIR}/privkey.pem ${RELAY_CERTS}/privkey.pem
chown -R ${RELAY_USER}:${RELAY_USER} ${RELAY_CERTS}
systemctl restart huzaa-relay 2>/dev/null || true
DEPLOY
chmod +x /etc/letsencrypt/renewal-hooks/deploy/huzaa-relay-reload.sh

systemctl daemon-reload
systemctl enable huzaa-relay

echo ""
echo "=== Huzaa relay installed at ${RELAY_HOME} ==="
echo "Config: ${RELAY_HOME}/relay.json"
echo "Start:  systemctl start huzaa-relay"
echo "Logs:   journalctl -u huzaa-relay -f"
echo "Relay listens on :5349 (TLS). Ensure firewall allows 5349 and 50000-50100."
if [[ ! -d "$CERT_DIR" ]]; then
  echo "Create a cert for ${RELAY_DOMAIN} and copy to ${RELAY_CERTS}/ then start the service."
fi
