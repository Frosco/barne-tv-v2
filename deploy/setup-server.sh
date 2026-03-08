#!/bin/bash
# One-time server setup - run as root on a fresh Hetzner instance.
set -euo pipefail

echo "==> Updating system"
apt update && apt upgrade -y

echo "==> Installing Caddy"
apt install -y debian-keyring debian-archive-keyring apt-transport-https curl
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' | tee /etc/apt/sources.list.d/caddy-stable.list
chmod o+r /usr/share/keyrings/caddy-stable-archive-keyring.gpg
chmod o+r /etc/apt/sources.list.d/caddy-stable.list
apt update
apt install -y caddy

echo "==> Configuring firewall"
apt install -y ufw
ufw allow 22/tcp
ufw allow 80/tcp
ufw allow 443/tcp
ufw --force enable

echo "==> Hardening SSH"
sed -i 's/^#\?PermitRootLogin.*/PermitRootLogin prohibit-password/' /etc/ssh/sshd_config
sed -i 's/^#\?PasswordAuthentication.*/PasswordAuthentication no/' /etc/ssh/sshd_config
systemctl restart ssh

echo "==> Installing fail2ban"
apt install -y fail2ban
systemctl enable fail2ban
systemctl start fail2ban

echo "==> Enabling unattended security upgrades"
apt install -y unattended-upgrades
echo 'Unattended-Upgrade::Automatic-Reboot "false";' > /etc/apt/apt.conf.d/51auto-upgrades
echo -e 'APT::Periodic::Update-Package-Lists "1";\nAPT::Periodic::Unattended-Upgrade "1";' > /etc/apt/apt.conf.d/20auto-upgrades

echo "==> Creating app user and directories"
useradd --system --no-create-home barnetv
mkdir -p /opt/barne-tv/templates /opt/barne-tv/static
chown -R barnetv:barnetv /opt/barne-tv

echo "==> Installing systemd service"
cp /tmp/barne-tv.service /etc/systemd/system/barne-tv.service
systemctl daemon-reload
systemctl enable barne-tv

echo "==> Stopping nginx if present"
systemctl stop nginx 2>/dev/null && systemctl disable nginx 2>/dev/null || true
apt remove -y nginx nginx-common 2>/dev/null || true

echo "==> Installing Caddyfile"
cp /tmp/Caddyfile /etc/caddy/Caddyfile
systemctl restart caddy

echo ""
echo "Setup complete. Next steps:"
echo "  1. Copy config.yaml to /opt/barne-tv/config.yaml (with your YouTube API key)"
echo "  2. Run the deploy script from your local machine"
