-- Keep the groups table in sync with the Ent Group schema used by API key queries.
ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS image_input_price_per_image NUMERIC(20,12);
