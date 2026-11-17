// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package buffer

import "testing"

func TestPostAssetInputsRejectCredentialBearingAndIncompleteMedia(t *testing.T) {
	tests := []struct {
		name  string
		asset PostAsset
	}{
		{name: "signed URL", asset: PostAsset{Type: "image", URL: "https://example.com/photo.jpg?token=secret"}},
		{name: "embedded credentials", asset: PostAsset{Type: "image", URL: "https://user:secret@example.com/photo.jpg"}},
		{name: "document without thumbnail", asset: PostAsset{Type: "document", URL: "https://example.com/report.pdf", Title: "Report"}},
		{name: "unknown type", asset: PostAsset{Type: "audio", URL: "https://example.com/audio.mp3"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := bufferPostAssetInputs([]PostAsset{test.asset}); err == nil {
				t.Fatal("expected media validation failure")
			}
		})
	}
}
