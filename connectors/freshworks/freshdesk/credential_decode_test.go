// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package freshdesk_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/freshworks/freshdesk"
)

func TestDecodeResolvedCredentialsJSONAcceptsOnlyAnAPIKey(t *testing.T) {
	credentials, err := freshdesk.DecodeResolvedCredentialsJSON(json.RawMessage(`{"api_key":"` + testAPIKey + `"}`))
	require.NoError(t, err)
	require.Equal(t, testAPIKey, credentials.APIKey.Reveal())

	for name, contents := range map[string]string{
		"unknown field":  `{"api_key":"` + testAPIKey + `","domain":"acme"}`,
		"missing key":    `{}`,
		"key with colon": `{"api_key":"` + testAPIKey + `:X"}`,
		"key with space": `{"api_key":"two words"}`,
		"not an object":  `["` + testAPIKey + `"]`,
	} {
		_, err := freshdesk.DecodeResolvedCredentialsJSON(json.RawMessage(contents))
		require.Error(t, err, name)
		require.NotContains(t, err.Error(), testAPIKey, name)
	}
}
