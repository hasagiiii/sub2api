//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestAdminAdjustOrganizationBalance(t *testing.T) {
	for _, tc := range []struct {
		name, before, amount, operation, after string
		wantErr                                error
	}{
		{"partial_negative_repayment", "-10.00000000", "3.00000000", "add", "-7.00000000", nil},
		{"repayment_to_zero", "-10.00000000", "10.00000000", "add", "0.00000000", nil},
		{"repayment_to_positive", "-10.00000000", "12.00000000", "add", "2.00000000", nil},
		{"positive_top_up", "2.00000000", "3.00000000", "add", "5.00000000", nil},
		{"subtract_to_zero", "3.00000000", "3.00000000", "subtract", "0.00000000", nil},
		{"subtract_with_remainder", "3.00000000", "2.00000000", "subtract", "1.00000000", nil},
		{"subtract_insufficient", "3.00000000", "3.00000001", "subtract", "3.00000000", service.ErrCompanyInsufficientBalance},
		{"subtract_from_negative", "-10.00000000", "1.00000000", "subtract", "-10.00000000", service.ErrCompanyInsufficientBalance},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateOrganizationIntegrationTest(t)
			ctx := context.Background()
			repo := NewOrganizationRepository(integrationDB)
			admin := createOrganizationRoot(t, integrationEntClient, 0, service.RoleAdmin)
			owner := createOrganizationRoot(t, integrationEntClient, 20, service.RoleUser)
			orgID := createActiveOrganization(t, owner, 20)
			_, err := integrationDB.ExecContext(ctx, `UPDATE organizations SET balance=$2::numeric WHERE id=$1`, orgID, tc.before)
			require.NoError(t, err)
			key := uuid.NewString()
			err = repo.AdminAdjustOrganizationBalance(ctx, admin.ID, orgID, tc.amount, tc.operation, key)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				require.NotErrorIs(t, err, service.ErrInsufficientBalance)
			} else {
				require.NoError(t, err)
				// Retrying a successful adjustment must not change the balance or duplicate records.
				require.NoError(t, repo.AdminAdjustOrganizationBalance(ctx, admin.ID, orgID, tc.amount, tc.operation, key))
			}
			assertOrganizationBalances(t, orgID, tc.after, "0.00000000")
			assertUserBalances(t, owner.ID, "20.00000000", "0.00000000")
			var ledgerCount, auditCount int
			require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT count(*) FROM organization_financial_ledger WHERE organization_id=$1 AND kind='admin_adjustment'`, orgID).Scan(&ledgerCount))
			require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT count(*) FROM organization_audit_events WHERE organization_id=$1 AND action='organization.balance.admin_adjust'`, orgID).Scan(&auditCount))
			if tc.wantErr != nil {
				require.Zero(t, ledgerCount)
				require.Zero(t, auditCount)
			} else {
				require.Equal(t, 1, ledgerCount)
				require.Equal(t, 1, auditCount)
				var ledgerBalance, ledgerAmount, auditOperation, auditBalance string
				require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT source_balance_after::text,amount::text FROM organization_financial_ledger WHERE idempotency_key=$1`, "organization:admin-balance:"+key).Scan(&ledgerBalance, &ledgerAmount))
				require.Equal(t, tc.after, ledgerBalance)
				require.Equal(t, tc.amount, ledgerAmount)
				require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT metadata->>'operation',(metadata->>'balance_after')::numeric(20,8)::text FROM organization_audit_events WHERE organization_id=$1 AND action='organization.balance.admin_adjust'`, orgID).Scan(&auditOperation, &auditBalance))
				require.Equal(t, tc.operation, auditOperation)
				require.Equal(t, tc.after, auditBalance)
			}
		})
	}
}

func TestAdminAdjustOrganizationBalance_MissingOrganization(t *testing.T) {
	isolateOrganizationIntegrationTest(t)
	ctx := context.Background()
	repo := NewOrganizationRepository(integrationDB)
	admin := createOrganizationRoot(t, integrationEntClient, 0, service.RoleAdmin)
	for _, operation := range []string{"add", "subtract"} {
		require.ErrorIs(t, repo.AdminAdjustOrganizationBalance(ctx, admin.ID, -1, "1.00000000", operation, uuid.NewString()), service.ErrCompanyNotFound)
	}
}
