// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package support_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zendesk/support"
)

func TestDecodeResolvedCredentialsJSONAcceptsExactlyTheAPITokenPair(t *testing.T) {
	credentials, err := support.DecodeResolvedCredentialsJSON(json.RawMessage(`{"email":"` + testEmail + `","api_token":"` + testAPIToken + `"}`))
	require.NoError(t, err)
	require.Equal(t, testEmail, credentials.Email)
	require.Equal(t, testAPIToken, credentials.APIToken.Reveal())

	for name, contents := range map[string]string{
		"unknown field":   `{"email":"` + testEmail + `","api_token":"` + testAPIToken + `","refresh_token":"x"}`,
		"missing token":   `{"email":"` + testEmail + `"}`,
		"missing email":   `{"api_token":"` + testAPIToken + `"}`,
		"display address": `{"email":"Agent <` + testEmail + `>","api_token":"` + testAPIToken + `"}`,
		"token space":     `{"email":"` + testEmail + `","api_token":"` + testAPIToken + ` x"}`,
		"not an object":   `"` + testAPIToken + `"`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := support.DecodeResolvedCredentialsJSON(json.RawMessage(contents))
			require.EqualError(t, err, "Zendesk resolved credential is invalid")
		})
	}
}
