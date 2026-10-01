// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package intercom_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/intercom"
)

func TestDecodeResolvedCredentialsJSONAcceptsOnlyTheDeclaredFields(t *testing.T) {
	credentials, err := intercom.DecodeResolvedCredentialsJSON(json.RawMessage(`{"access_token":"` + testAccessToken + `","client_secret":"` + testClientSecret + `"}`))
	require.NoError(t, err)
	require.Equal(t, testAccessToken, credentials.AccessToken.Reveal())
	require.Equal(t, testClientSecret, credentials.ClientSecret.Reveal())

	withoutSecret, err := intercom.DecodeResolvedCredentialsJSON(json.RawMessage(`{"access_token":"` + testAccessToken + `"}`))
	require.NoError(t, err)
	require.Empty(t, withoutSecret.ClientSecret.Reveal(), "client_secret is needed only for webhooks")

	for name, contents := range map[string]string{
		"missing token":  `{"client_secret":"` + testClientSecret + `"}`,
		"unknown field":  `{"access_token":"` + testAccessToken + `","region":"eu"}`,
		"unsafe token":   `{"access_token":"token with space"}`,
		"not an object":  `["` + testAccessToken + `"]`,
		"header newline": `{"access_token":"token\nInjected: yes"}`,
	} {
		_, err := intercom.DecodeResolvedCredentialsJSON(json.RawMessage(contents))
		require.Error(t, err, name)
		require.NotContains(t, err.Error(), testAccessToken, name)
		require.NotContains(t, err.Error(), testClientSecret, name)
	}
}
