#!/bin/bash
REPO="ibreakthecloud/kiwi"

echo "Creating Milestones..."
gh api repos/$REPO/milestones -f title="Phase P1: Control-plane foundation" > /dev/null
gh api repos/$REPO/milestones -f title="Phase P2: Master/Worker agents + checkpointing" > /dev/null
gh api repos/$REPO/milestones -f title="Phase P3: Security & trust boundary hardening" > /dev/null
gh api repos/$REPO/milestones -f title="Phase P4: Observability & scale" > /dev/null
gh api repos/$REPO/milestones -f title="Phase P5: Governance & extensibility" > /dev/null
gh api repos/$REPO/milestones -f title="Phase P6: Deployment Strategy & BYOC Runner" > /dev/null

echo "Assigning issues to milestones..."
# P1 issues
for id in $(gh issue list --search "in:title P1." --json number -q '.[].number'); do
  gh issue edit $id -m "Phase P1: Control-plane foundation"
done

# P2 issues
for id in $(gh issue list --search "in:title P2." --json number -q '.[].number'); do
  gh issue edit $id -m "Phase P2: Master/Worker agents + checkpointing"
done

# P3 issues
for id in $(gh issue list --search "in:title P3." --json number -q '.[].number'); do
  gh issue edit $id -m "Phase P3: Security & trust boundary hardening"
done

# P4 issues
for id in $(gh issue list --search "in:title P4." --json number -q '.[].number'); do
  gh issue edit $id -m "Phase P4: Observability & scale"
done

# P5 issues
for id in $(gh issue list --search "in:title P5." --json number -q '.[].number'); do
  gh issue edit $id -m "Phase P5: Governance & extensibility"
done

# P6 issues (D1, D2, D3, D4)
for id in $(gh issue list --search "in:title D1: OR in:title D2: OR in:title D3: OR in:title D4:" --json number -q '.[].number'); do
  gh issue edit $id -m "Phase P6: Deployment Strategy & BYOC Runner"
done

echo "Closing old Epic issues..."
for id in $(gh issue list --search "in:title \"Phase P1:\" OR in:title \"Phase P2:\" OR in:title \"Phase P3:\" OR in:title \"Phase P4:\" OR in:title \"Phase P5:\" OR in:title \"Phase P6:\" OR in:title \"[EPIC]\"" --json number -q '.[].number'); do
  gh issue close $id -r "not planned" -c "Converted to GitHub Milestone for better project board visualization."
done

echo "Done!"
