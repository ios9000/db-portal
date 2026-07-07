#!/usr/bin/env bash
# Bootstrap a fresh Ubuntu 24.04 VM as the db-portal dev environment (ADR-009).
# Idempotent: safe to re-run. Run as the login user (needs passwordless sudo).
#
# Full flow when standing up a NEW VM (driven from the workstation):
#   1. scp this script over (or curl it from the repo) and run it on the VM.
#   2. It prints a deploy PUBLIC key at the end. Register it from the workstation:
#        gh repo deploy-key add <pubkey-file> --repo ios9000/db-portal \
#          --allow-write --title "dbportal-vm (<hostname>)"
#      (Revoke the previous VM's key: gh repo deploy-key list/delete.)
#   3. On the VM: git clone git@github.com:ios9000/db-portal.git ~/db-portal
#      NOTE: the github.com ssh stanza written below uses ssh.github.com:443
#      because provider networks often block outbound port 22.
#   4. Optional: sudo npm install -g @anthropic-ai/claude-code && claude login
#   5. Re-login once so docker group membership takes effect.
set -euo pipefail

echo "== base packages =="
sudo DEBIAN_FRONTEND=noninteractive apt-get update -qq
sudo DEBIAN_FRONTEND=noninteractive apt-get install -y -qq git curl ca-certificates

echo "== docker =="
if ! command -v docker >/dev/null; then
  curl -fsSL https://get.docker.com | sudo sh
fi
sudo usermod -aG docker "$USER"

echo "== node 22 =="
if ! command -v node >/dev/null || [ "$(node -v | cut -d. -f1)" != "v22" ]; then
  curl -fsSL https://deb.nodesource.com/setup_22.x | sudo -E bash -
  sudo DEBIAN_FRONTEND=noninteractive apt-get install -y -qq nodejs
fi

echo "== go (ADR-010) =="
if ! command -v /usr/local/go/bin/go >/dev/null; then
  GOVER=$(curl -fsSL "https://go.dev/VERSION?m=text" | head -1)
  curl -fsSL -o /tmp/go.tgz "https://go.dev/dl/${GOVER}.linux-amd64.tar.gz"
  sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf /tmp/go.tgz && rm /tmp/go.tgz
  printf 'export PATH=$PATH:/usr/local/go/bin:$HOME/go/bin\n' | sudo tee /etc/profile.d/go.sh >/dev/null
fi
export PATH=$PATH:/usr/local/go/bin:$HOME/go/bin

echo "== golangci-lint =="
command -v golangci-lint >/dev/null || curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/HEAD/install.sh | sudo sh -s -- -b /usr/local/bin

echo "== git identity + github over 443 =="
git config --global user.name  >/dev/null 2>&1 || git config --global user.name "Archer"
git config --global user.email >/dev/null 2>&1 || git config --global user.email "ios900070025@proton.me"
mkdir -p ~/.ssh && chmod 700 ~/.ssh
if ! grep -q "Host github.com" ~/.ssh/config 2>/dev/null; then
  printf 'Host github.com\n  HostName ssh.github.com\n  Port 443\n  IdentityFile ~/.ssh/dbportal_deploy\n  IdentitiesOnly yes\n  StrictHostKeyChecking accept-new\n' >> ~/.ssh/config
fi

echo "== deploy key =="
[ -f ~/.ssh/dbportal_deploy ] || ssh-keygen -t ed25519 -N '' -f ~/.ssh/dbportal_deploy -C "dbportal-deploy@$(hostname)"

echo "== VERSIONS =="
git --version; docker --version; docker compose version
node -v; npm -v; go version; golangci-lint version

echo "== smoke =="
sudo docker run --rm hello-world | grep -m1 "Hello from Docker"

echo "== REGISTER THIS DEPLOY PUBKEY (see header, step 2) =="
cat ~/.ssh/dbportal_deploy.pub
echo "BOOTSTRAP-DONE"
