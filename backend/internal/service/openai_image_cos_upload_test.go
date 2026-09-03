package service

import (
	"encoding/base64"
	"testing"
)

func TestDecodeOpenAIImageBase64(t *testing.T) {
	want := []byte("image-bytes")
	encoded := base64.StdEncoding.EncodeToString(want)
	for _, payload := range []string{
		encoded,
		base64.RawStdEncoding.EncodeToString(want),
		"data:image/png;base64," + encoded,
	} {
		got, err := decodeOpenAIImageBase64(payload)
		if err != nil {
			t.Fatalf("decode %q: %v", payload[:min(len(payload), 24)], err)
		}
		if string(got) != string(want) {
			t.Fatalf("decoded %q, want %q", got, want)
		}
	}
}
