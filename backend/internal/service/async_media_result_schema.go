package service

import (
	"fmt"
	"regexp"
	"strings"
)

var asyncResultPathToken = regexp.MustCompile(`[^.\[\]]+|\[([^\]]+)\]`)

// extractConfiguredImageURLs extracts media URLs from a native provider
// payload using the model intro's result_field first, then declared output
// fields as a compatibility fallback.
func extractConfiguredImageURLs(payload map[string]any, intro *ModelIntro) (urls []string, matchedFields []string) {
	if payload == nil || intro == nil {
		return nil, nil
	}
	paths := make([]string, 0, len(intro.OutputFields)+1)
	if field := strings.TrimSpace(intro.ResultField); field != "" {
		paths = append(paths, field)
	} else {
		for _, field := range intro.OutputFields {
			key := strings.TrimSpace(field.Key)
			if key == "" {
				continue
			}
			leaf := key
			if index := strings.LastIndexAny(leaf, ".[]"); index >= 0 {
				leaf = leaf[index+1:]
			}
			if strings.EqualFold(leaf, "url") || strings.EqualFold(field.Type, "image") || strings.EqualFold(field.Type, "video") {
				paths = append(paths, key)
			}
		}
	}
	seenPath := make(map[string]struct{}, len(paths))
	seenURL := make(map[string]struct{})
	for _, field := range paths {
		if _, ok := seenPath[field]; ok {
			continue
		}
		seenPath[field] = struct{}{}
		values := asyncResultValuesByPath(payload, field)
		fieldURLs := make([]string, 0, len(values))
		for _, value := range values {
			collectAsyncResultURLs(value, &fieldURLs)
		}
		if len(fieldURLs) == 0 {
			continue
		}
		matchedFields = append(matchedFields, field)
		for _, url := range fieldURLs {
			if _, ok := seenURL[url]; ok {
				continue
			}
			seenURL[url] = struct{}{}
			urls = append(urls, url)
		}
	}
	return urls, matchedFields
}

func asyncResultValuesByPath(payload map[string]any, field string) []any {
	path := strings.TrimSpace(field)
	if path == "" {
		return nil
	}
	path = strings.ReplaceAll(path, "[\\*]", "[*]")
	path = strings.ReplaceAll(path, "[]", "[*]")
	tokens := make([]string, 0, 8)
	for _, match := range asyncResultPathToken.FindAllStringSubmatch(path, -1) {
		if match[1] != "" {
			tokens = append(tokens, match[1])
		} else {
			tokens = append(tokens, match[0])
		}
	}
	return asyncWalkResultPath(payload, tokens)
}

func asyncWalkResultPath(value any, tokens []string) []any {
	if len(tokens) == 0 {
		return []any{value}
	}
	if value == nil {
		return nil
	}
	head, rest := tokens[0], tokens[1:]
	switch current := value.(type) {
	case map[string]any:
		if head == "*" {
			var out []any
			for _, child := range current {
				out = append(out, asyncWalkResultPath(child, rest)...)
			}
			return out
		}
		return asyncWalkResultPath(current[head], rest)
	case []any:
		if head == "*" {
			var out []any
			for _, child := range current {
				out = append(out, asyncWalkResultPath(child, rest)...)
			}
			return out
		}
		var index int
		if _, err := fmt.Sscanf(head, "%d", &index); err != nil || index < 0 || index >= len(current) {
			return nil
		}
		return asyncWalkResultPath(current[index], rest)
	default:
		return nil
	}
}

func collectAsyncResultURLs(value any, out *[]string) {
	switch current := value.(type) {
	case string:
		if strings.TrimSpace(current) != "" {
			*out = append(*out, strings.TrimSpace(current))
		}
	case map[string]any:
		if raw, ok := current["url"].(string); ok && strings.TrimSpace(raw) != "" {
			*out = append(*out, strings.TrimSpace(raw))
		}
	case []any:
		for _, child := range current {
			collectAsyncResultURLs(child, out)
		}
	}
}

// extractConfiguredImageMetadata preserves provider metadata from the raw
// object that contains each extracted URL. This keeps native result responses
// useful even when the optional COS transfer cannot download the image.
func extractConfiguredImageMetadata(payload map[string]any, urls []string) []ImageOutputMetadata {
	if payload == nil || len(urls) == 0 {
		return nil
	}
	metadata := make([]ImageOutputMetadata, len(urls))
	for i, rawURL := range urls {
		rawURL = strings.TrimSpace(rawURL)
		metadata[i].URL = rawURL
		if image := findAsyncImageObject(payload, rawURL); image != nil {
			metadata[i].ContentType = asyncStringValue(image["content_type"])
			metadata[i].FileName = asyncStringValue(image["file_name"])
			metadata[i].FileSize = asyncInt64Value(image["file_size"])
			metadata[i].Width = asyncIntValue(image["width"])
			metadata[i].Height = asyncIntValue(image["height"])
		}
		if metadata[i].FileName == "" {
			metadata[i].FileName = imageFileNameFromURL(rawURL)
		}
	}
	return metadata
}

func imageOutputSizesFromMetadata(metadata []ImageOutputMetadata) []string {
	if len(metadata) == 0 {
		return nil
	}
	sizes := make([]string, len(metadata))
	for i, item := range metadata {
		if item.Width > 0 && item.Height > 0 {
			sizes[i] = fmt.Sprintf("%dx%d", item.Width, item.Height)
		}
	}
	return sizes
}

func findAsyncImageObject(value any, targetURL string) map[string]any {
	switch current := value.(type) {
	case map[string]any:
		if candidate, ok := current["url"].(string); ok && strings.TrimSpace(candidate) == targetURL {
			return current
		}
		for _, child := range current {
			if found := findAsyncImageObject(child, targetURL); found != nil {
				return found
			}
		}
	case []any:
		for _, child := range current {
			if found := findAsyncImageObject(child, targetURL); found != nil {
				return found
			}
		}
	}
	return nil
}

func asyncStringValue(value any) string {
	if raw, ok := value.(string); ok {
		return strings.TrimSpace(raw)
	}
	return ""
}

func asyncIntValue(value any) int {
	switch raw := value.(type) {
	case int:
		return raw
	case int8:
		return int(raw)
	case int16:
		return int(raw)
	case int32:
		return int(raw)
	case int64:
		return int(raw)
	case uint:
		return int(raw)
	case uint8:
		return int(raw)
	case uint16:
		return int(raw)
	case uint32:
		return int(raw)
	case uint64:
		return int(raw)
	case float64:
		return int(raw)
	case float32:
		return int(raw)
	default:
		return 0
	}
}

func asyncInt64Value(value any) int64 {
	switch raw := value.(type) {
	case int:
		return int64(raw)
	case int8:
		return int64(raw)
	case int16:
		return int64(raw)
	case int32:
		return int64(raw)
	case int64:
		return raw
	case uint:
		return int64(raw)
	case uint8:
		return int64(raw)
	case uint16:
		return int64(raw)
	case uint32:
		return int64(raw)
	case uint64:
		return int64(raw)
	case float64:
		return int64(raw)
	case float32:
		return int64(raw)
	default:
		return 0
	}
}
