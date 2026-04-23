# adblock-proxy

A lightweight HTTP sidecar that embeds `adblock-rust` and exposes network
filtering as a JSON API. Called by the Incus runner executor, workspace Nginx
proxy, and GitLab Nginx proxy to filter outbound/inbound URLs against
EasyList/uBlock Origin-compatible filter lists.

## API

| Method | Path      | Purpose                                      |
|--------|-----------|----------------------------------------------|
| `POST` | `/check`  | Check whether a URL should be blocked        |
| `POST` | `/reload` | Reload filter lists from disk without restart|
| `GET`  | `/health` | Liveness probe                               |
| `GET`  | `/stats`  | Engine statistics                            |

### `POST /check`

```json
{
  "url": "https://example.com/ad.js",
  "source_url": "https://gitlab.local/mygroup/myrepo",
  "resource_type": "script"
}
```

Response:

```json
{
  "blocked": true,
  "matched_rule": "||example.com/ad.js^",
  "redirect": null
}
```

`resource_type` values: `script`, `image`, `stylesheet`, `xmlhttprequest`,
`subdocument`, `websocket`, `media`, `font`, `other`

## Build

```bash
cd utils/adblock-proxy
cargo build --release
sudo install -m 755 target/release/adblock-proxy /usr/local/bin/
```

## Filter lists

Place EasyList-format `.txt` files in `/etc/adblock-proxy/lists/`:

```bash
sudo mkdir -p /etc/adblock-proxy/lists
# EasyList (ads)
sudo curl -fsSL https://easylist.to/easylist/easylist.txt \
  -o /etc/adblock-proxy/lists/easylist.txt
# EasyPrivacy (trackers)
sudo curl -fsSL https://easylist.to/easylist/easyprivacy.txt \
  -o /etc/adblock-proxy/lists/easyprivacy.txt
# uBlock Origin filters
sudo curl -fsSL https://raw.githubusercontent.com/uBlockOrigin/uAssets/master/filters/filters.txt \
  -o /etc/adblock-proxy/lists/ublock-filters.txt
```

## Running as a systemd service

```bash
sudo tee /etc/systemd/system/adblock-proxy.service <<'EOF'
[Unit]
Description=adblock-proxy filter sidecar
After=network.target

[Service]
ExecStart=/usr/local/bin/adblock-proxy \
  --listen 127.0.0.1:6060 \
  --lists-dir /etc/adblock-proxy/lists
Restart=always
RestartSec=3
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
EOF
sudo systemctl daemon-reload
sudo systemctl enable --now adblock-proxy
```

## Integration points

### CI runner (outbound filtering)

In `runtime/incus/runner/run.sh`, before executing job scripts:

```bash
# Check if the job script fetches a blocked URL
if curl -sf -X POST http://127.0.0.1:6060/check \
  -H 'Content-Type: application/json' \
  -d "{\"url\":\"$FETCH_URL\",\"resource_type\":\"xmlhttprequest\"}" \
  | grep -q '"blocked":true'; then
  echo "Blocked by adblock-proxy: $FETCH_URL" >&2
  exit 1
fi
```

### Nginx proxy (workspace/GitLab)

Use `ngx_http_mirror_module` or a Lua script to check URLs via adblock-proxy
before proxying. See `deploy/ansible/roles/nginx/` for the Nginx configuration.

### Automatic filter list updates

Add a systemd timer to refresh lists weekly:

```bash
sudo tee /etc/systemd/system/adblock-proxy-update.service <<'EOF'
[Unit]
Description=Update adblock-proxy filter lists

[Service]
Type=oneshot
ExecStart=/bin/bash -c '\
  curl -fsSL https://easylist.to/easylist/easylist.txt \
    -o /etc/adblock-proxy/lists/easylist.txt && \
  curl -fsSL https://easylist.to/easylist/easyprivacy.txt \
    -o /etc/adblock-proxy/lists/easyprivacy.txt && \
  curl -sf -X POST http://127.0.0.1:6060/reload'
EOF

sudo tee /etc/systemd/system/adblock-proxy-update.timer <<'EOF'
[Unit]
Description=Weekly adblock filter list update

[Timer]
OnCalendar=weekly
Persistent=true

[Install]
WantedBy=timers.target
EOF
sudo systemctl enable --now adblock-proxy-update.timer
```
