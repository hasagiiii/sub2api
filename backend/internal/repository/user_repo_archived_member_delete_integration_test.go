//go:build integration

package repository

import (
	"context"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/memberpolicyattachment"
	"github.com/Wei-Shaw/sub2api/ent/organizationmembership"
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUserRepository_DeleteArchivedIAMMember(t *testing.T) {
	for _, entry := range []string{"admin", "organization", "repository", "rollback"} {
		t.Run(entry, func(t *testing.T) {
			isolateOrganizationIntegrationTest(t)
			ctx := context.Background()
			client := testEntClient(t)
			userRepo := NewUserRepository(client, integrationDB)
			keyRepo := NewAPIKeyRepository(client, integrationDB)
			orgRepo := NewOrganizationRepository(integrationDB)
			admin := service.NewAdminService(
				userRepo, nil, nil, nil, keyRepo, nil, nil, nil, nil, nil, nil,
				nil, client, nil, nil, nil, nil, nil, nil, nil, nil, nil,
			)
			orgService := service.NewOrganizationService(orgRepo, userRepo, nil)
			owner := createOrganizationRoot(t, client, 100, service.RoleUser)
			orgID := createActiveOrganization(t, owner, 20)
			memberID := createIAMMemberForOrganizationTest(t, owner.ID, "delete-member")
			peerID := createIAMMemberForOrganizationTest(t, owner.ID, "keep-member")
			key := mustCreateApiKey(t, client, &service.APIKey{UserID: memberID})
			peerKey := mustCreateApiKey(t, client, &service.APIKey{UserID: peerID})

			membership, err := client.OrganizationMembership.Query().Where(organizationmembership.UserIDEQ(memberID)).Only(ctx)
			require.NoError(t, err)
			peerMembership, err := client.OrganizationMembership.Query().Where(organizationmembership.UserIDEQ(peerID)).Only(ctx)
			require.NoError(t, err)
			var policyID int64
			require.NoError(t, integrationDB.QueryRowContext(ctx,
				`SELECT id FROM managed_policies WHERE policy_key=$1`, service.PolicyCompanyFinanceReadOnly).Scan(&policyID))
			// Both active and detached bindings of the deleted membership must go.
			_, err = integrationDB.ExecContext(ctx, `
				INSERT INTO member_policy_attachments
				(organization_id,membership_id,policy_id,policy_version,attached_by_user_id,detached_by_user_id,detached_at)
				VALUES ($1,$2,$3,1,$4,$5,NOW()), ($1,$2,$3,1,$4,NULL,NULL), ($1,$6,$3,1,$5,$5,NOW()), ($1,$6,$3,1,$5,NULL,NULL)`,
				orgID, membership.ID, policyID, owner.ID, memberID, peerMembership.ID)
			require.NoError(t, err)
			// Include all historical user-ID references, including an IAM operator.
			_, err = integrationDB.ExecContext(ctx, `
				INSERT INTO organization_financial_ledger
				(idempotency_key,kind,organization_id,actor_user_id,source_user_id,destination_user_id,amount,source_balance_after,destination_balance_after)
				VALUES ('delete-member-allocate','allocate',$1,$2,$3,$2,1,99,1),
				       ('delete-member-reclaim','reclaim',$1,$2,$2,$3,1,0,100)`, orgID, memberID, owner.ID)
			require.NoError(t, err)
			_, err = integrationDB.ExecContext(ctx, `
				INSERT INTO organization_audit_events(organization_id,actor_user_id,subject_user_id,action,result)
				VALUES($1,$2,$2,'iam.test','success')`, orgID, memberID)
			require.NoError(t, err)
			require.NoError(t, orgRepo.SetIAMMemberStatus(ctx, owner.ID, memberID, service.MembershipStatusArchived))

			if entry == "rollback" {
				tx := testEntTx(t)
				txCtx := dbent.NewTxContext(ctx, tx)
				require.NoError(t, keyRepo.DeleteWithAudit(txCtx, key.ID))
				require.NoError(t, userRepo.Delete(txCtx, memberID))
				_, err = tx.Client().User.Get(mixins.SkipSoftDelete(txCtx), memberID)
				require.True(t, dbent.IsNotFound(err))
				require.NoError(t, tx.Rollback())
				archived, err := userRepo.GetByIDIncludeDeleted(ctx, memberID)
				require.NoError(t, err)
				require.NotNil(t, archived.DeletedAt)
				_, err = client.OrganizationMembership.Get(ctx, membership.ID)
				require.NoError(t, err)
				count, err := client.MemberPolicyAttachment.Query().Where(memberpolicyattachment.MembershipIDEQ(membership.ID)).Count(ctx)
				require.NoError(t, err)
				require.Equal(t, 2, count)
				_, err = client.APIKey.Get(ctx, key.ID)
				require.NoError(t, err)
				return
			}

			switch entry {
			case "admin":
				err = admin.DeleteUser(ctx, memberID)
			case "organization":
				// The enterprise endpoint must still enforce ownership and archival.
				require.ErrorIs(t, orgService.DeleteArchivedIAMMember(ctx, peerID, memberID, admin), service.ErrOrganizationPermission)
				require.Error(t, orgService.DeleteArchivedIAMMember(ctx, owner.ID, peerID, admin))
				otherOwner := createOrganizationRoot(t, client, 100, service.RoleUser)
				createActiveOrganization(t, otherOwner, 20)
				require.ErrorIs(t, orgService.DeleteArchivedIAMMember(ctx, otherOwner.ID, memberID, admin), service.ErrIAMMemberNotFound)
				err = orgService.DeleteArchivedIAMMember(ctx, owner.ID, memberID, admin)
			case "repository":
				err = userRepo.Delete(ctx, memberID)
			}
			require.NoError(t, err)
			_, err = userRepo.GetByIDIncludeDeleted(ctx, memberID)
			require.ErrorIs(t, err, service.ErrUserNotFound)
			require.ErrorIs(t, admin.DeleteUser(ctx, memberID), service.ErrUserNotFound)
			_, err = client.APIKey.Get(mixins.SkipSoftDelete(ctx), key.ID)
			require.True(t, dbent.IsNotFound(err))
			_, err = client.OrganizationMembership.Get(ctx, membership.ID)
			require.True(t, dbent.IsNotFound(err))
			count, err := client.MemberPolicyAttachment.Query().Where(memberpolicyattachment.MembershipIDEQ(membership.ID)).Count(ctx)
			require.NoError(t, err)
			require.Zero(t, count)

			// Other members and their policies/keys remain usable.
			_, err = userRepo.GetByID(ctx, peerID)
			require.NoError(t, err)
			_, err = userRepo.GetByID(ctx, owner.ID)
			require.NoError(t, err)
			_, err = client.APIKey.Get(ctx, peerKey.ID)
			require.NoError(t, err)
			count, err = client.MemberPolicyAttachment.Query().Where(memberpolicyattachment.MembershipIDEQ(peerMembership.ID)).Count(ctx)
			require.NoError(t, err)
			require.Equal(t, 2, count)
			members, _, err := orgRepo.ListIAMMembers(ctx, owner.ID)
			require.NoError(t, err)
			require.Len(t, members, 1)
			require.Equal(t, peerID, members[0].UserID)
			var ledgerCount int
			require.NoError(t, integrationDB.QueryRowContext(ctx,
				`SELECT count(*) FROM organization_financial_ledger WHERE organization_id=$1 AND actor_user_id=$2 AND (source_user_id=$2 OR destination_user_id=$2)`, orgID, memberID).Scan(&ledgerCount))
			require.Equal(t, 2, ledgerCount)
			violations, err := orgRepo.Reconcile(ctx)
			require.NoError(t, err)
			require.Zero(t, violations["transfer_conservation_violation"])
			events, _, err := orgRepo.ListAuditEvents(ctx, orgID, service.OrganizationAuditFilter{})
			require.NoError(t, err)
			require.NotEmpty(t, events)
			var auditCount int
			require.NoError(t, integrationDB.QueryRowContext(ctx,
				`SELECT count(*) FROM organization_audit_events WHERE organization_id=$1 AND actor_user_id=$2 AND subject_user_id=$2`, orgID, memberID).Scan(&auditCount))
			require.Equal(t, 1, auditCount)
		})
	}
}

func TestUserRepository_DeleteLiveUserStillSoftDeletes(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	repo := NewUserRepository(tx.Client(), integrationDB)
	user := mustCreateUser(t, tx.Client(), &service.User{})
	require.NoError(t, repo.Delete(ctx, user.ID))
	archived, err := repo.GetByIDIncludeDeleted(ctx, user.ID)
	require.NoError(t, err)
	require.NotNil(t, archived.DeletedAt)
	require.NoError(t, repo.Delete(ctx, user.ID))
	_, err = repo.GetByIDIncludeDeleted(ctx, user.ID)
	require.ErrorIs(t, err, service.ErrUserNotFound)
}
