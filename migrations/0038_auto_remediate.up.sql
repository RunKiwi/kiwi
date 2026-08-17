--
-- Whether a REGRESSION verdict from Post-Merge Verification auto-spawns a fix
-- task via the same continuation path PR-comment fixes use. Off by default:
-- an agent autonomously pushing more production changes off its own
-- regression detection needs a human opt-in until verdict quality is proven.
ALTER TABLE organizations ADD COLUMN IF NOT EXISTS auto_remediate BOOLEAN NOT NULL DEFAULT false;
