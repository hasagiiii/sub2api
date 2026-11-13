-- Allow administrators to record direct organization wallet adjustments.
ALTER TABLE organization_financial_ledger
    DROP CONSTRAINT IF EXISTS organization_financial_ledger_kind_check;
ALTER TABLE organization_financial_ledger
    ADD CONSTRAINT organization_financial_ledger_kind_check
    CHECK (kind IN ('upgrade_reserve', 'upgrade_capture', 'upgrade_release', 'allocate', 'reclaim', 'company_deposit', 'company_withdraw', 'admin_adjustment'));
