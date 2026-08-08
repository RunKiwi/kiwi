-- A short, provider-supplied description of what a model is for.
--
-- The model picker previously offered 100+ ids with nothing to choose between
-- them. This is the provider's own words rather than anything Kiwi writes: for
-- a catalog this size, hand-written blurbs would be stale within a month and
-- wrong for models nobody here has used.
--
-- Nullable-by-default (empty string) because only OpenRouter supplies one; the
-- native providers' list endpoints return ids alone.
ALTER TABLE model_catalog ADD COLUMN IF NOT EXISTS description TEXT NOT NULL DEFAULT '';
