-- API key wallet fallback preference.
-- When an enterprise subscription is exhausted, this controls whether normal
-- balance billing prefers the company's wallet over the owner's personal wallet.
ALTER TABLE api_keys
    ADD COLUMN IF NOT EXISTS prefer_company_balance BOOLEAN NOT NULL DEFAULT FALSE;
