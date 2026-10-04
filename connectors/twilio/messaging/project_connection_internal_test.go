// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package messaging

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

// The identifiers are split so that secret scanners do not mistake these fixtures for real credentials.
const (
	fakeAPIKeySID  = "SK" + "0123456789abcdef0123456789abcdef"
	fakeAccountSID = "AC" + "0123456789abcdef0123456789abcdef"
)

// TestProjectConnectionReadsEachAuthenticationMethod decodes each method's Dex Web records, as NewProjectConnection does.
func TestProjectConnectionReadsEachAuthenticationMethod(t *testing.T) {
	records := []struct {
		connectionName string
		settings       string
		credentials    string
	}{
		{
			connectionName: "twilio-auth-token",
			settings:       `{"accountSid":"` + fakeAccountSID + `","defaultSender":"+14155550100"}`,
			credentials:    `{"auth_method":"auth-token","auth_token":"auth-token-sentinel-0123456789ab"}`,
		},
		{
			connectionName: "twilio-api-key",
			settings:       `{"accountSid":"` + fakeAccountSID + `","defaultSender":"MG00112233445566778899aabbccddeeff"}`,
			credentials:    `{"auth_method":"api-key","api_key_sid":"` + fakeAPIKeySID + `","api_key_secret":"api-key-secret-sentinel-012345678"}`,
		},
	}
	var configuration projectconfig.Configuration
	for _, record := range records {
		configuration.Connections = append(configuration.Connections, projectconfig.ConnectionConfiguration{
			ConnectorID: ConnectorID, ConnectionName: record.connectionName,
			ModulePath: "github.com/superdurable/dex-connectors-library/connectors/twilio/messaging", Provider: "twilio",
			Configuration: json.RawMessage(record.settings),
		})
	}
	for _, record := range records {
		var config Config
		key := projectconfig.ConnectionKey{ConnectorID: ConnectorID, ConnectionName: record.connectionName}
		require.NoError(t, configuration.DecodeConnectionConfiguration(key, &config), record.connectionName)
		credentials, err := decodeCredentials(json.RawMessage(record.credentials))
		require.NoError(t, err, record.connectionName)
		reference := sdkgo.ConnectionRef{Provider: "twilio", Name: record.connectionName}
		client, err := New(config, sdkgo.StaticCredentialProvider[Credentials]{reference: credentials})
		require.NoError(t, err, record.connectionName)
		_, err = NewConnection(client, reference)
		require.NoError(t, err, record.connectionName)
	}

	var config Config
	missing := projectconfig.ConnectionKey{ConnectorID: ConnectorID, ConnectionName: "missing"}
	require.ErrorIs(t, configuration.DecodeConnectionConfiguration(missing, &config), projectconfig.ErrObjectNotFound)
	for _, contents := range []string{
		`{}`,
		`{"auth_token":"auth-token-sentinel-0123456789ab"}`,
		`{"auth_method":"api-key","api_key_secret":"api-key-secret-sentinel-012345678"}`,
		`{"auth_method":"auth-token","auth_token":"auth-token-sentinel-0123456789ab","account_sid":"` + fakeAccountSID + `"}`,
	} {
		_, err := decodeCredentials(json.RawMessage(contents))
		require.Error(t, err, contents)
	}
}
