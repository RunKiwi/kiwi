#!/bin/bash
set -e

if [ -z "$KIWI_SERVER_TOKEN" ]; then
  echo "Error: KIWI_SERVER_TOKEN must be set"
  exit 1
fi

KIWI_URL=${KIWI_URL:-"http://localhost:8080"}

echo "Creating initial organization..."
ORG_RES=$(curl -s -X POST "$KIWI_URL/admin/orgs" \
  -H "Authorization: Bearer $KIWI_SERVER_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name": "Initial Org"}')

ORG_ID=$(echo "$ORG_RES" | grep -o '"id":"[^"]*' | cut -d'"' -f4)
if [ -z "$ORG_ID" ]; then
  echo "Failed to create organization. Response: $ORG_RES"
  exit 1
fi
echo "✅ Organization created: $ORG_ID"

echo "Creating admin user..."
USER_RES=$(curl -s -X POST "$KIWI_URL/admin/orgs/$ORG_ID/users" \
  -H "Authorization: Bearer $KIWI_SERVER_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"email": "admin@example.com", "name": "Admin", "role": "admin"}')

USER_ID=$(echo "$USER_RES" | grep -o '"id":"[^"]*' | cut -d'"' -f4)
if [ -z "$USER_ID" ]; then
  echo "Failed to create user. Response: $USER_RES"
  exit 1
fi
echo "✅ Admin user created: $USER_ID"

echo "Generating API key..."
KEY_RES=$(curl -s -X POST "$KIWI_URL/admin/orgs/$ORG_ID/users/$USER_ID/keys" \
  -H "Authorization: Bearer $KIWI_SERVER_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"label": "bootstrap-admin"}')

API_KEY=$(echo "$KEY_RES" | grep -o '"key":"[^"]*' | cut -d'"' -f4)
if [ -z "$API_KEY" ]; then
  echo "Failed to generate API key. Response: $KEY_RES"
  exit 1
fi
echo "✅ API Key generated!"

echo ""
echo "=================================================="
echo "Bootstrap complete! Store this API key securely:"
echo "KIWI_ORG_ID: $ORG_ID"
echo "KIWI_API_KEY: $API_KEY"
echo "=================================================="
echo ""
