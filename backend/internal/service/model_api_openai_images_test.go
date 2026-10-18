//go:build unit

package service

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestOpenAIImageAliasWhitelistAllowsCanonicalTransport(t *testing.T) {
	for _, model := range modelAPIOpenAIImageModels {
		for _, suffix := range []string{"", "/edit"} {
			alias := "openai/" + model + suffix
			t.Run(alias, func(t *testing.T) {
				raw := map[string]any{alias: alias}
				account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"model_mapping": raw}}
				require.True(t, account.IsModelSupported(model))
				require.Equal(t, model, account.GetMappedModel(alias))
				require.Equal(t, alias, raw[alias], "stored credentials remain unchanged")
				require.Contains(t, ModelAPIOpenAIImageSlugs(account), alias)
				require.False(t, account.IsModelSupported("gpt-6"))
			})
		}
	}
}

func TestOpenAIImageAliasesPreserveExplicitMappingsAndOtherProviders(t *testing.T) {
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"model_mapping": map[string]any{
		"gpt-image-2": "gpt-image-2.5-flare", "openai/gpt-image-2": "openai/gpt-image-2", "openai/gpt-image-2.5-sunburst": "custom-target",
	}}}
	require.Equal(t, "gpt-image-2.5-flare", account.GetMappedModel("gpt-image-2"))
	require.Equal(t, "custom-target", account.GetMappedModel("openai/gpt-image-2.5-sunburst"))
	fal := &Account{Platform: PlatformFal, Credentials: map[string]any{"model_mapping": map[string]any{"openai/gpt-image-2/edit": "openai/gpt-image-2/edit"}}}
	require.Equal(t, "openai/gpt-image-2/edit", fal.GetMappedModel("openai/gpt-image-2/edit"))
	require.Empty(t, ModelAPIOpenAIImageSlugs(fal))
}

func TestOpenAIPlaygroundCatalogExcludesTextOnlyAccounts(t *testing.T) {
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"model_mapping": map[string]any{"gpt-6": "gpt-6"}}}
	require.Empty(t, ModelAPIOpenAIImageSlugs(account))
	account.Credentials = nil
	require.Len(t, ModelAPIOpenAIImageSlugs(account), 9)
}
