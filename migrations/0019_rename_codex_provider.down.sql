-- Deliberately a no-op. Reverting would restore a provider value that no code
-- path recognises, turning working rows back into unselectable ones. The
-- forward migration only renames a label to the one the runtime implements.
SELECT 1;
