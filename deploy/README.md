# Kiwi Deployment Runbook

This runbook will guide you through setting up a single-VM deployment of Kiwi, including automatic HTTPS via Caddy and one managed execution daemon.

## Prerequisites
- A virtual machine (VM) with Docker and Docker Compose installed.
- A domain name pointing to your VM's public IP address.

## Step-by-Step Setup

### 1. Provision and Configure Environment
SSH into your VM, clone the repository, and prepare your environment variables.

```bash
git clone https://github.com/RunKiwi/kiwi.git
cd kiwi/deploy
cp .env.example .env
```

Edit the `.env` file to set your configuration.
```bash
nano .env
```
Ensure you generate secure random strings for `KIWI_ENCRYPTION_KEY` and `KIWI_SERVER_TOKEN`:
```bash
openssl rand -hex 32
```
And set your `DOMAIN` and `KIWI_CORS_ALLOWED_ORIGINS` correctly. (Do not set `KIWI_JOIN_TOKEN` yet).

### 2. Start the Stack (Without Daemon)
Start Postgres, Kiwid (Control Plane), and Caddy (Reverse Proxy).

```bash
docker compose -f docker-compose.prod.yml up -d postgres kiwid caddy
```
Verify the control plane is healthy:
```bash
curl https://your-domain.com/readyz
```
It should return `{"status":"ok"}`.

### 3. Bootstrap First Organization and API Key
Use the bootstrap script to create the initial organization, an admin user, and your first API key.

```bash
export KIWI_SERVER_TOKEN="<your-server-token>"
export KIWI_URL="https://your-domain.com"
./bootstrap.sh
```
Save the `KIWI_ORG_ID` and `KIWI_API_KEY` output securely.

### 4. Register the Managed Daemon
We need to generate a join token so the daemon can authenticate with the control plane.
Using the `KIWI_API_KEY` obtained in step 3:

```bash
curl -s -X POST "https://your-domain.com/api/v1/daemon/join-token" \
  -H "Authorization: Bearer $KIWI_API_KEY"
```
Take the `join_token` from the response, and update your `.env` file:
```bash
KIWI_JOIN_TOKEN="<the-token>"
```

Start the daemon:
```bash
docker compose -f docker-compose.prod.yml up -d kiwidaemon
```

### 5. Verify the Setup
Run a sample submission using the Kiwi CLI (assuming the CLI is installed and configured).

```bash
kiwi submit -server "https://your-domain.com" -token "<your-api-key>" \
  -repo "https://github.com/you/yourrepo" -task "Fix the failing test" \
  -file path/to/file.go -test-cmd "go test ./..."
```

## Local setup (no domain / no TLS)

To run the whole stack on your machine, use the local override — it publishes
`kiwid` on `localhost:8080` and skips Caddy:

```bash
# from the repo root, with deploy/.env filled in (see .env.example)
docker compose -f deploy/docker-compose.prod.yml -f deploy/docker-compose.local.yml \
  --env-file deploy/.env up -d --build postgres kiwid kiwidaemon
curl http://localhost:8080/readyz     # {"status":"ok"}
```

Then run `bootstrap.sh` (with `KIWI_URL=http://localhost:8080`), mint a join token,
put it in `deploy/.env`, and re-up `kiwidaemon`.
