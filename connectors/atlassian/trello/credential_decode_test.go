// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package trello_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/trello"
)

func TestHostedCredentialDecodingAcceptsOnlyTheKeyAndToken(t *testing.T) {
	credentials, err := trello.DecodeResolvedCredentialsJSON(json.RawMessage(`{"api_key":"` + testAPIKey + `","token":"` + testToken + `"}`))
	require.NoError(t, err)
	require.Equal(t, testAPIKey, credentials.APIKey.Reveal())
	require.Equal(t, testToken, credentials.Token.Reveal())
	for _, contents := range []string{
		`{"api_key":"` + testAPIKey + `","token":"` + testToken + `","secret":"oauth-secret"}`,
		`{"api_key":"` + testAPIKey + `"}`,
		`{"api_key":"` + testAPIKey + `","token":""}`,
		`{"api_key":"` + testAPIKey + `","token":"line\nbreak"}`,
		`{"api_key":"` + testAPIKey + `","token":"quote\"break"}`,
		`[]`,
	} {
		_, err := trello.DecodeResolvedCredentialsJSON(json.RawMessage(contents))
		require.Error(t, err, contents)
		require.NotContains(t, err.Error(), testAPIKey)
		require.NotContains(t, err.Error(), testToken)
	}
}
