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
curl -s -X POST "https://your-domain.com/api/v1/daemons/tokens" \
  -H "Authorization: Bearer $KIWI_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"expires_in": "24h"}'
```
Take the `token` from the response, and update your `.env` file:
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
export KIWI_API_KEY="<your-api-key>"
export KIWI_SERVER_URL="https://your-domain.com"
kiwi submit -repo "RunKiwi/kiwi" -prompt "Say hello world"
```
