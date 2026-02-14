# Huzaa relay

Relay for IRC file-sharing: runs on the IRC server, accepts bot connections (TLS) and user DCC connections (TCP/TLS), and forwards file data between them. Used together with [huzaa-bot](https://github.com/awgh/huzaa-bot).

## Build

```bash
go build -o relay ./cmd/relay
```

## Config

Copy `config/relay.json.sample` to `config/relay.json` and set:

- `relay_host` – hostname to advertise (e.g. irc.example.com)
- `tls_cert_file`, `tls_key_file` – TLS for bot and user DCC (SDCC)
- `dcc_port_min`, `dcc_port_max` – port range for user DCC connections
- `turn_users` – list of `{ "username", "secret" }` allowed to connect. Auth is required: every bot must send this credential as the first message. To revoke a bot, remove its entry and restart the relay. If empty, the relay logs a warning and all auth will fail.

## Run

```bash
./relay -config config/relay.json
```

## Deploy on IONOS VPS

The script `install-relay.sh` installs the relay on a Debian VPS (e.g. IONOS) with systemd, Let's Encrypt certs, and a certbot deploy hook.

**Prereqs**

- Same host as Ergo (or at least Let's Encrypt certs for your IRC/relay domain).
- DNS for the relay host (e.g. `irc.example.com`) points to the server.
- Ports **5349** (relay TLS) and **50000–50100** (DCC) open in firewall.

**Install (as root)**

```bash
# From your machine: copy script to VPS and run
scp install-relay.sh root@irc.example.com:/root/
ssh root@irc.example.com 'bash /root/install-relay.sh'
```

Optional: use a different domain (e.g. relay on a subdomain):

```bash
RELAY_DOMAIN=relay.example.com bash /root/install-relay.sh
```

**After install**

- Config: `/opt/huzaa-relay/relay.json`
- Start: `systemctl start huzaa-relay`
- Logs: `journalctl -u huzaa-relay -f`
- Cert renewal: certbot deploy hook copies new certs and restarts the relay.

If the server didn’t have a cert yet, create one (e.g. run a VPS setup that does certbot for your domain), then copy certs into `/opt/huzaa-relay/certs/` and start the service.

## Protocol

The bot must send MsgAuth (username + secret) as the first frame; the relay responds with MsgAuthOk or MsgError. Then RegisterDownload / RegisterUpload (session + filename), relay replies with PortAlloc (port). File bytes are sent as Data frames until EOF. Same frame format is used by the fileshare bot; keep both repos in sync if you change the protocol.
