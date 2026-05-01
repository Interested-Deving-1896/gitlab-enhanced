# garm-gitlab host setup

This guide covers deploying garm-gitlab on a dedicated Incus host (architecture B)
so it can manage runner pools for GitLab CI jobs — including privileged containers
for live-build.

## Requirements

- Debian 12 / Ubuntu 24.04 host (bare-metal or VM with nested virt enabled)
- Incus installed and initialised
- Go 1.22+ (build only — not needed at runtime)
- Outbound HTTPS to gitlab.com (or your GitLab instance)
- Inbound HTTPS reachable by GitLab for webhooks (see [Webhook access](#webhook-access))

## 1. Install Incus

```sh
curl -fsSL https://pkgs.zabbly.com/get/incus-stable | sh
incus admin init --minimal
```

For live-build pools, enable unprivileged user namespaces if not already on:

```sh
echo 'kernel.unprivileged_userns_clone=1' > /etc/sysctl.d/99-userns.conf
sysctl --system
```

## 2. Create Incus profiles

Standard runner profile (resource limits, no privilege):

```sh
incus profile create gitlab-runner
incus profile set gitlab-runner limits.cpu 2
incus profile set gitlab-runner limits.memory 4GB
incus profile set gitlab-runner security.privileged false
```

Privileged profile for live-build:

```sh
incus profile create live-build
incus profile set live-build limits.cpu 4
incus profile set live-build limits.memory 8GB
incus profile set live-build security.privileged true
incus profile set live-build security.nesting true
```

## 3. Install garm-gitlab

```sh
git clone https://gitlab.com/openos-project/git-management_deving/gitlab-enhanced.git
cd gitlab-enhanced/ci/runners/garm-gitlab
sudo ./deploy/install.sh
```

This will:
- Build the binary (requires Go)
- Install it to `/usr/local/bin/garm-gitlab`
- Create the `garm-gitlab` system user (added to the `incus` group)
- Write an example config to `/etc/garm-gitlab/config.toml.example`
- Install the systemd unit

## 4. Configure

```sh
sudo cp /etc/garm-gitlab/config.toml.example /etc/garm-gitlab/config.toml
sudo editor /etc/garm-gitlab/config.toml
```

Key values to fill in:

| Key | Where to get it |
|---|---|
| `gitlab.token` | GitLab → User Settings → Access Tokens (scope: `api`) |
| `pool.registration_token` | GitLab → Group/Project → Settings → CI/CD → Runners → Registration token |
| `api.webhook_secret` | Any random string — must match the GitLab webhook config |
| `api.listen_address` | Usually `0.0.0.0:8080`; adjust if behind a reverse proxy |

## 5. Webhook access

GitLab must be able to POST to `http(s)://<your-host>:8080/webhook`.

### Option A — cloudflared tunnel (no public IP needed)

```sh
# Install cloudflared
curl -fsSL https://pkg.cloudflare.com/cloudflare-main.gpg \
  | gpg --dearmor > /usr/share/keyrings/cloudflare-main.gpg
echo 'deb [signed-by=/usr/share/keyrings/cloudflare-main.gpg] \
  https://pkg.cloudflare.com/cloudflared bookworm main' \
  > /etc/apt/sources.list.d/cloudflared.list
apt-get update && apt-get install -y cloudflared

# Authenticate and create a named tunnel
cloudflared tunnel login
cloudflared tunnel create garm-gitlab
cloudflared tunnel route dns garm-gitlab garm-gitlab.<your-domain>

# Create tunnel config
cat > /etc/cloudflared/config.yml << EOF
tunnel: garm-gitlab
credentials-file: /root/.cloudflared/<tunnel-id>.json
ingress:
  - hostname: garm-gitlab.<your-domain>
    service: http://localhost:8080
  - service: http_status:404
EOF

# Install and start as a service
cloudflared service install
systemctl enable --now cloudflared
```

The webhook URL will be `https://garm-gitlab.<your-domain>/webhook`.

### Option B — nginx reverse proxy with Let's Encrypt

```sh
apt-get install -y nginx certbot python3-certbot-nginx
certbot --nginx -d garm-gitlab.<your-domain>
```

`/etc/nginx/sites-available/garm-gitlab`:

```nginx
server {
    listen 443 ssl;
    server_name garm-gitlab.<your-domain>;

    location /webhook {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }
}
```

## 6. Register the GitLab webhook

In your GitLab project or group:
**Settings → Webhooks → Add new webhook**

- **URL**: `https://garm-gitlab.<your-domain>/webhook`
- **Secret token**: value of `api.webhook_secret` in config.toml
- **Trigger**: ✅ Job events only
- **SSL verification**: ✅ enabled (if using a valid cert)

## 7. Start garm-gitlab

```sh
sudo cp /etc/garm-gitlab/config.toml.example /etc/garm-gitlab/config.toml
# edit config.toml ...
sudo systemctl enable --now garm-gitlab
sudo systemctl status garm-gitlab
sudo journalctl -u garm-gitlab -f
```

## 8. Verify

Trigger a CI job with tags matching one of your pools. You should see:

```
[INFO] received job event  build_id=12345 build_status=pending tags=linux,incus
[INFO] creating Incus instance  instance_id=garm-gitlab-ubuntu-noble-...
[INFO] instance ready  instance_id=... runner_id=...
```

And `incus list` should show the new container.

## Troubleshooting

| Symptom | Likely cause |
|---|---|
| Webhook returns 401 | `webhook_secret` mismatch between config and GitLab |
| No instances created | Pool tags don't match job tags; check logs |
| Instance stuck in Starting | Image not cached locally — first pull takes time |
| live-build fails inside container | Profile missing `security.privileged=true` |
| garm-gitlab can't reach Incus | `garm-gitlab` user not in `incus` group — run `usermod -aG incus garm-gitlab` and restart |
