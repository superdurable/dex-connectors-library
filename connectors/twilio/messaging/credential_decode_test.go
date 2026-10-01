// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package messaging_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/twilio/messaging"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
)

func TestDecodeResolvedCredentialsJSONSelectsTheBrokerMethod(t *testing.T) {
	testCases := []struct {
		name     string
		contents string
		methodID string
		secret   func(messaging.Credentials) string
	}{
		{"explicit auth token", `{"auth_method":"auth-token","auth_token":"` + testAuthToken + `"}`, messaging.AuthTokenAuthMethodID,
			func(credentials messaging.Credentials) string { return credentials.AuthToken.Reveal() }},
		{"inferred auth token", `{"auth_token":"` + testAuthToken + `"}`, messaging.AuthTokenAuthMethodID,
			func(credentials messaging.Credentials) string { return credentials.AuthToken.Reveal() }},
		{"inferred API key", `{"api_key_sid":"` + testAPIKeySID + `","api_key_secret":"` + testAPIKeySecret + `"}`, messaging.APIKeyAuthMethodID,
			func(credentials messaging.Credentials) string {
				return credentials.APIKeySID + credentials.APIKeySecret.Reveal()
			}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			credentials, err := messaging.DecodeResolvedCredentialsJSON(json.RawMessage(testCase.contents))
			require.NoError(t, err)
			require.Equal(t, testCase.methodID, credentials.AuthMethodID)
			require.NotEmpty(t, testCase.secret(credentials))
		})
	}
}

func TestDecodeResolvedCredentialsJSONRejectsAmbiguousOrUnknownMaterial(t *testing.T) {
	for _, contents := range []string{
		`{}`,
		`{"auth_method":"auth-token"}`,
		`{"api_key_secret":"` + testAPIKeySecret + `"}`,
		`{"auth_method":"api-key","auth_token":"` + testAuthToken + `"}`,
		`{"auth_token":"` + testAuthToken + `","api_key_secret":"` + testAPIKeySecret + `"}`,
		`{"auth_token":"` + testAuthToken + `","account_sid":"` + testAccountSID + `"}`,
		`{"auth_method":"oauth","auth_token":"` + testAuthToken + `"}`,
		`not json ` + testAuthToken,
	} {
		_, err := messaging.DecodeResolvedCredentialsJSON(json.RawMessage(contents))
		require.Error(t, err, contents)
		require.NotContains(t, err.Error(), testAuthToken)
		require.NotContains(t, err.Error(), testAPIKeySecret)
	}
}

func TestNewLocalConnectionLoadsEachAuthenticationMethod(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connections.json")
	connection := func(name string, configuration map[string]any, credentials map[string]any) map[string]any {
		return map[string]any{
			"connectorId": messaging.ConnectorID, "modulePath": "github.com/superdurable/dex-connectors-library/connectors/twilio/messaging",
			"moduleVersion": "v0.1.0", "provider": "twilio", "connectionName": name,
			"configuration": configuration, "credentials": credentials,
		}
	}
	contents, err := json.Marshal(map[string]any{
		"schemaVersion": localconfig.SchemaVersion,
		"connections": []any{
			connection("twilio-auth-token",
				map[string]any{"accountSid": testAccountSID, "defaultSender": testSender},
				map[string]any{"auth_method": messaging.AuthTokenAuthMethodID, "auth_token": testAuthToken}),
			connection("twilio-api-key",
				map[string]any{"accountSid": testAccountSID, "defaultSender": testServiceSID},
				map[string]any{"auth_method": messaging.APIKeyAuthMethodID, "api_key_sid": testAPIKeySID, "api_key_secret": testAPIKeySecret}),
		},
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, contents, 0o600))
	store, err := localconfig.LoadFile(path)
	require.NoError(t, err)
	for _, connectionName := range []string{"twilio-auth-token", "twilio-api-key"} {
		_, err := messaging.NewLocalConnection(store, connectionName)
		require.NoError(t, err, connectionName)
	}
	_, err = messaging.NewLocalConnection(store, "missing")
	require.Error(t, err)
}
