-- Restoring the constraint would re-break every telemetry write, so this is
-- deliberately a no-op. The forward migration only removes a reference to a
-- table the live path stopped using.
SELECT 1;
