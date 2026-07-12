#!/bin/bash
echo "Creating Epic..."
epic_id=$(gh issue create --title "[EPIC] v2-beta: Agentic Execution Platform Migration" --body "Tracking epic for migrating Kiwi to the v2 distributed architecture." | grep -o 'https://github.com/ibreakthecloud/kiwi/issues/[0-9]*' | awk -F/ '{print $NF}')
echo "Epic created: $epic_id"

echo "Creating Phase 1..."
p1_id=$(gh issue create --title "Phase P1: Control-plane foundation" --body "Replace SQLite with Postgres, add transactional outbox, JetStream queue, split API/LLMO roles, and formalize Docker driver. (Belongs to EPIC #$epic_id)" | grep -o 'https://github.com/ibreakthecloud/kiwi/issues/[0-9]*' | awk -F/ '{print $NF}')
gh issue create --title "P1.1: Postgres store + migrations" --body "Belongs to #$p1_id" > /dev/null
gh issue create --title "P1.2: Transactional outbox + relay" --body "Belongs to #$p1_id" > /dev/null
gh issue create --title "P1.3: Durable queue (NATS JetStream)" --body "Belongs to #$p1_id" > /dev/null
gh issue create --title "P1.4: API server role" --body "Belongs to #$p1_id" > /dev/null
gh issue create --title "P1.5: LLMO consumer + manifest generation" --body "Belongs to #$p1_id" > /dev/null
gh issue create --title "P1.6: Infrastructure driver interface (Docker)" --body "Belongs to #$p1_id" > /dev/null

echo "Creating Phase 2..."
p2_id=$(gh issue create --title "Phase P2: Master/Worker agents + checkpointing" --body "Add Sandbox Agent API, Master/Worker topology, event log, S3 checkpoints, and side-effect ledger. (Belongs to EPIC #$epic_id)" | grep -o 'https://github.com/ibreakthecloud/kiwi/issues/[0-9]*' | awk -F/ '{print $NF}')
gh issue create --title "P2.1: Sandbox Agent API (gRPC)" --body "Belongs to #$p2_id" > /dev/null
gh issue create --title "P2.2: Master + Workers topology" --body "Belongs to #$p2_id" > /dev/null
gh issue create --title "P2.3: Event log + object-store checkpoints" --body "Belongs to #$p2_id" > /dev/null
gh issue create --title "P2.4: Side-effect ledger + rollback/resume" --body "Belongs to #$p2_id" > /dev/null

echo "Creating Phase 3..."
p3_id=$(gh issue create --title "Phase P3: Security & trust boundary hardening" --body "Add JIT broker, egress allowlist, manifest integrity, and stronger isolation options. (Belongs to EPIC #$epic_id)" | grep -o 'https://github.com/ibreakthecloud/kiwi/issues/[0-9]*' | awk -F/ '{print $NF}')
gh issue create --title "P3.1: JIT secret broker" --body "Belongs to #$p3_id" > /dev/null
gh issue create --title "P3.2: Egress allowlist enforcement" --body "Belongs to #$p3_id" > /dev/null
gh issue create --title "P3.3: Manifest integrity (schemas & signing)" --body "Belongs to #$p3_id" > /dev/null
gh issue create --title "P3.4: Stronger isolation driver (gVisor/Firecracker)" --body "Belongs to #$p3_id" > /dev/null

echo "Creating Phase 4..."
p4_id=$(gh issue create --title "Phase P4: Observability & scale" --body "End-to-end OpenTelemetry, Event bus, live streaming, metrics, and fair queuing. (Belongs to EPIC #$epic_id)" | grep -o 'https://github.com/ibreakthecloud/kiwi/issues/[0-9]*' | awk -F/ '{print $NF}')
gh issue create --title "P4.1: OpenTelemetry end-to-end" --body "Belongs to #$p4_id" > /dev/null
gh issue create --title "P4.2: Event bus & live SSE streaming" --body "Belongs to #$p4_id" > /dev/null
gh issue create --title "P4.3: Metrics & dashboards" --body "Belongs to #$p4_id" > /dev/null
gh issue create --title "P4.4: Fair queuing + backpressure" --body "Belongs to #$p4_id" > /dev/null
gh issue create --title "P4.5: Fleet-scale Infra drivers (K8s/E2B)" --body "Belongs to #$p4_id" > /dev/null

echo "Creating Phase 5..."
p5_id=$(gh issue create --title "Phase P5: Governance & extensibility" --body "Multi-provider LLM layer (BYO-LLM), budgets/quotas, and template authoring. (Belongs to EPIC #$epic_id)" | grep -o 'https://github.com/ibreakthecloud/kiwi/issues/[0-9]*' | awk -F/ '{print $NF}')
gh issue create --title "P5.1: Multi-provider LLM layer (BYO-LLM)" --body "Belongs to #$p5_id" > /dev/null
gh issue create --title "P5.2: Budgets/quotas + cost UI" --body "Belongs to #$p5_id" > /dev/null
gh issue create --title "P5.3: Template authoring & workflow registry APIs" --body "Belongs to #$p5_id" > /dev/null
gh issue create --title "P5.4: 3rd-party manifest plugins" --body "Belongs to #$p5_id" > /dev/null

echo "Creating Phase 6..."
p6_id=$(gh issue create --title "Phase P6: Deployment Strategy & BYOC Runner" --body "Implement Docker Compose local dev and BYOC Runner Daemon. (Belongs to EPIC #$epic_id)" | grep -o 'https://github.com/ibreakthecloud/kiwi/issues/[0-9]*' | awk -F/ '{print $NF}')
gh issue create --title "D1: Local Compose Stack (docker-compose)" --body "Belongs to #$p6_id" > /dev/null
gh issue create --title "D2: Kiwi Runner Daemon binary" --body "Belongs to #$p6_id" > /dev/null
gh issue create --title "D3: SaaS Runner Orchestration" --body "Belongs to #$p6_id" > /dev/null
gh issue create --title "D4: Native Cloud Secrets (OIDC)" --body "Belongs to #$p6_id" > /dev/null

echo "All issues created successfully."
