-- These user IDs describe historical actors and transfer participants, not
-- live authorization relationships. Keep them (and the ledger/audit rows)
-- when an archived IAM user is permanently deleted. Nulling transfer IDs
-- would also break transfer-conservation checks and idempotency history.
ALTER TABLE organization_financial_ledger
    DROP CONSTRAINT IF EXISTS organization_financial_ledger_actor_user_id_fkey,
    DROP CONSTRAINT IF EXISTS organization_financial_ledger_source_user_id_fkey,
    DROP CONSTRAINT IF EXISTS organization_financial_ledger_destination_user_id_fkey;

ALTER TABLE organization_audit_events
    DROP CONSTRAINT IF EXISTS organization_audit_events_actor_user_id_fkey,
    DROP CONSTRAINT IF EXISTS organization_audit_events_subject_user_id_fkey;

-- An operator's deletion must not revoke policies they attached to other
-- members. The target membership and policy foreign keys remain enforced.
ALTER TABLE member_policy_attachments
    DROP CONSTRAINT IF EXISTS member_policy_attachments_attached_by_user_id_fkey,
    DROP CONSTRAINT IF EXISTS member_policy_attachments_detached_by_user_id_fkey;

COMMENT ON COLUMN organization_financial_ledger.actor_user_id IS 'Historical actor user ID; retained after user deletion';
COMMENT ON COLUMN organization_financial_ledger.source_user_id IS 'Historical source user ID; retained after user deletion';
COMMENT ON COLUMN organization_financial_ledger.destination_user_id IS 'Historical destination user ID; retained after user deletion';
COMMENT ON COLUMN organization_audit_events.actor_user_id IS 'Historical actor user ID; retained after user deletion';
COMMENT ON COLUMN organization_audit_events.subject_user_id IS 'Historical subject user ID; retained after user deletion';
COMMENT ON COLUMN member_policy_attachments.attached_by_user_id IS 'Historical attaching user ID; retained after user deletion';
COMMENT ON COLUMN member_policy_attachments.detached_by_user_id IS 'Historical detaching user ID; retained after user deletion';
