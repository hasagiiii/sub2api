package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGroupSeedreamImagePricingMigration(t *testing.T) {
	content, err := FS.ReadFile("241_add_group_seedream_image_input_price.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql,
		"ALTER TABLE groups ADD COLUMN IF NOT EXISTS image_input_price_per_image NUMERIC(20,12)")
}
