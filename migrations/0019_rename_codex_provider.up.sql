-- The Models page offered a "Codex" provider that nothing implemented.
--
-- An org could select it when adding a model, so the row was stored with
-- provider = 'codex'. Nothing routed on that value: the daemon picks the
-- provider from the model id, and the dashboard filters the model list to
-- providers with a connected integration — and there was no 'codex'
-- integration to connect. The result is a model that is added, listed, and
-- never selectable.
--
-- OpenAI is now implemented under its own name, so these rows are remapped
-- rather than deleted: the model id is whatever the user typed, and it is very
-- likely a gpt-* id that will now route correctly.
--
-- org_models exists only through AutoMigrate (see the schema drift note in
-- CLAUDE.md §1), so a numbered migration cannot assume it is present — the
-- migrate role may run before any serving process has AutoMigrated. Guarded
-- rather than assumed.
DO $$
BEGIN
  IF to_regclass('public.org_models') IS NOT NULL THEN
    UPDATE org_models SET provider = 'openai' WHERE provider = 'codex';
  END IF;
END $$;
