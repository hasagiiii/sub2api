package admin

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIQTestModelsExcludeImageOnlyModels(t *testing.T) {
	models := iqTestModels()
	require.NotEmpty(t, models)

	for _, model := range models {
		require.NotContains(t, model.ID, "gpt-image-")
	}
}

func TestResolveIQTestModel(t *testing.T) {
	model, ok := resolveIQTestModel("")
	require.True(t, ok)
	require.Equal(t, defaultIQTestModel, model)

	model, ok = resolveIQTestModel("gpt-5.4")
	require.True(t, ok)
	require.Equal(t, "gpt-5.4", model)

	model, ok = resolveIQTestModel("provider/custom-model")
	require.True(t, ok)
	require.Equal(t, "provider/custom-model", model)

	model, ok = resolveIQTestModel("model with spaces")
	require.True(t, ok)
	require.Equal(t, "model with spaces", model)
}
