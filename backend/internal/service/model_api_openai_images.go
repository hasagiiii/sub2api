package service

import "strings"

var modelAPIOpenAIImageModels = [...]string{"gpt-image-2", "gpt-image-2.5-sunburst", "gpt-image-2.5-flare"}

// ModelAPIOpenAIImageModel resolves the public generation/edit slugs used by the
// unified model facade. The operation suffix is handled separately by the caller.
func ModelAPIOpenAIImageModel(model string) (string, bool) {
	normalized := strings.TrimSpace(model)
	normalized = strings.TrimPrefix(normalized, "openai/")
	normalized = strings.TrimSuffix(normalized, "/edit")
	for _, canonical := range modelAPIOpenAIImageModels {
		if normalized == canonical {
			return canonical, true
		}
	}
	return model, false
}

// Only OpenAI accounts consume the canonical model; FAL and other providers
// retain their own endpoint slugs and operation-specific mappings.
func modelAPIImageAccountModel(account *Account, model string) string {
	if account != nil && account.Platform == PlatformOpenAI {
		if canonical, ok := ModelAPIOpenAIImageModel(model); ok {
			return canonical
		}
	}
	return model
}

// ModelAPIOpenAIImageSlugs exposes only image models the account actually allows.
// Include canonical names for existing links and both unified-facade operations.
func ModelAPIOpenAIImageSlugs(account *Account) []string {
	if account == nil || account.Platform != PlatformOpenAI || !account.SupportsOpenAIImageCapability(OpenAIImagesCapabilityBasic) {
		return nil
	}
	models := make([]string, 0, len(modelAPIOpenAIImageModels)*3)
	for _, model := range modelAPIOpenAIImageModels {
		if !account.IsModelSupported(model) || !IsGPTImageGenerationModel(account.GetMappedModel(model)) {
			continue
		}
		models = append(models, model, "openai/"+model, "openai/"+model+"/edit")
	}
	return models
}

// The whitelist editor persists selected aliases as identity mappings. Normalize
// those aliases and allow their canonical transport model without rewriting an
// administrator's explicit mapping or mutating stored credentials.
func ensureOpenAIModelAPIImageMappings(mapping map[string]string) {
	for _, model := range modelAPIOpenAIImageModels {
		for _, alias := range []string{"openai/" + model, "openai/" + model + "/edit"} {
			target, exists := mapping[alias]
			if !exists || target != alias && target != model {
				continue
			}
			mapping[alias] = model
			if _, configured := mapping[model]; !configured {
				mapping[model] = model
			}
		}
	}
}
