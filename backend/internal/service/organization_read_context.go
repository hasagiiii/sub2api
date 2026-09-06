package service

import "context"

type organizationReadContextKey struct{}

type organizationReadContext struct {
	userID       int64
	organization *OrganizationContext
}

// WithOrganizationReadContext shares the authenticated organization snapshot
// within a read-only request. Mutations must still load current database state.
func WithOrganizationReadContext(ctx context.Context, userID int64, organization *OrganizationContext) context.Context {
	return context.WithValue(ctx, organizationReadContextKey{}, organizationReadContext{userID, organization})
}

func OrganizationReadContextFromContext(ctx context.Context, userID int64) (*OrganizationContext, bool) {
	value, ok := ctx.Value(organizationReadContextKey{}).(organizationReadContext)
	if !ok || value.userID != userID || value.organization == nil {
		return nil, false
	}
	return value.organization, true
}
