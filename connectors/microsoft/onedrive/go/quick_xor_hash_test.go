// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package onedrive

import (
	"encoding/base64"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/onedrive/internal/graphfake"
)

// quickXorHashVectors come from rclone's backend/onedrive/quickxorhash tests (MIT
// licensed), which were checked against OneDrive; sizes above 160 cover wraparound.
var quickXorHashVectors = []struct {
	base64Input string
	want        string
}{
	{"", "AAAAAAAAAAAAAAAAAAAAAAAAAAA="},
	{"Sg==", "SgAAAAAAAAAAAAAAAQAAAAAAAAA="},
	{"tbQ=", "taAFAAAAAAAAAAAAAgAAAAAAAAA="},
	{"T6LYJIfDh81JrAK309H2JMJTXis=", "zBTHrspn3mEcohlJdIUAbjGNaNg="},
	{"DWAAX5/CIfrmErgZa8ot6ZraeSbu", "LR2Z0PjuRYGKQB/mhQAuMrAGZbQ="},
	{"0n7nl3YJtipy6yeUbVPWtc2h45WbF9u8hTz5tNwj3dZZwfXWkk+GN3g=", "YJLNK7JR64j9aODWfqDvEe/u6NU="},
	{"qwGf2ESubE5jOUHHyc94ORczFYYbc2OmEzo+hBIyzJiNwAzC8PvJqtTzwkWkSslgHFGWQZR2BV5+uYTrYT7HVwRM40vqfj0dBgeDENyTenIOL1LHkjtDKoXEnQ0mXAHoJ8PjbNC93zi5TovVRXTNzfGEs5dpWVqxUzb5lc7dwkyvOluBw482mQ4xrzYyIY1t+//OrNi1ObGXuUw2jBQOFfJVj2Y6BOyYmfB1y36eBxi3zxeG5d5NYjm2GSh6e08QMAwu3zrINcqIzLOuNIiGXBtl7DjKt7b5wqi4oFiRpZsCyx2smhSrdrtK/CkdU6nDN+34vSR/M8rZpWQdBE7a8g==", "WYT9JY3JIo/pEBp+tIM6Gt2nyTM="},
	{"w0LGhqU1WXFbdavqDE4kAjEzWLGGzmTNikzqnsiXHx2KRReKVTxkv27u3UcEz9+lbMvYl4xFf2Z4aE1xRBBNd1Ke5C0zToSaYw5o4B/7X99nKK2/XaUX1byLow2aju2XJl2OpKpJg+tSJ2fmjIJTkfuYUz574dFX6/VXxSxwGH/xQEAKS5TCsBK3CwnuG1p5SAsQq3gGVozDWyjEBcWDMdy8/AIFrj/y03Lfc/RNRCQTAfZbnf2QwV7sluw4fH3XJr07UoD0YqN+7XZzidtrwqMY26fpLZnyZjnBEt1FAZWO7RnKG5asg8xRk9YaDdedXdQSJAOy6bWEWlABj+tVAigBxavaluUH8LOj+yfCFldJjNLdi90fVHkUD/m4Mr5OtmupNMXPwuG3EQlqWUVpQoYpUYKLsk7a5Mvg6UFkiH596y5IbJEVCI1Kb3D1", "e3+wo77iKcILiZegnzyUNcjCdoQ="},
}

func TestQuickXorHashMatchesPublishedVectors(t *testing.T) {
	for _, vector := range quickXorHashVectors {
		content, err := base64.StdEncoding.DecodeString(vector.base64Input)
		require.NoError(t, err)
		require.Equal(t, vector.want, computeQuickXorHash(content), "input of %d bytes", len(content))
		require.Equal(t, vector.want, graphfake.QuickXorHash(content), "fake, input of %d bytes", len(content))
	}
}

// The fake ports Microsoft's C# cell structure; the connector folds bytes into a bit array.
func TestQuickXorHashAgreesWithTheReferenceStructureOnRandomContent(t *testing.T) {
	random := rand.New(rand.NewPCG(7, 11))
	for _, size := range []int{1, 7, 8, 19, 20, 21, 63, 64, 159, 160, 161, 320, 1000, 4099, 70000} {
		content := make([]byte, size)
		for index := range content {
			content[index] = byte(random.UintN(256))
		}
		require.Equal(t, graphfake.QuickXorHash(content), computeQuickXorHash(content), "size %d", size)
	}
}
