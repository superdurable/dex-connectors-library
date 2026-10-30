// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package snowflake

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

// TestProjectConnectionReadsEachAuthenticationMethod decodes each method's Dex Web records, as NewProjectConnection does.
func TestProjectConnectionReadsEachAuthenticationMethod(t *testing.T) {
	records := []struct {
		connectionName string
		settings       string
		credentials    string
	}{
		{
			connectionName: "snowflake-key-pair",
			settings:       `{"accountIdentifier":"myorg-analytics","warehouse":"REPORTING_WH","statementTimeoutSeconds":1800}`,
			credentials:    `{"auth_method":"key-pair","user":"DEX_SERVICE","private_key":"private-key-sentinel"}`,
		},
		{
			connectionName: "snowflake-token",
			settings:       `{"accountIdentifier":"xy12345.us-east-2.aws","role":"DEX_REPORTING","maxRows":50}`,
			credentials:    `{"auth_method":"programmatic-access-token","programmatic_access_token":"token-sentinel"}`,
		},
	}
	var configuration projectconfig.Configuration
	for _, record := range records {
		configuration.Connections = append(configuration.Connections, projectconfig.ConnectionConfiguration{
			ConnectorID: ConnectorID, ConnectionName: record.connectionName,
			ModulePath: "github.com/superdurable/dex-connectors-library/connectors/snowflake", Provider: "snowflake",
			Configuration: json.RawMessage(record.settings),
		})
	}
	for _, record := range records {
		var config Config
		key := projectconfig.ConnectionKey{ConnectorID: ConnectorID, ConnectionName: record.connectionName}
		require.NoError(t, configuration.DecodeConnectionConfiguration(key, &config), record.connectionName)
		credentials, err := decodeCredentials(json.RawMessage(record.credentials))
		require.NoError(t, err, record.connectionName)
		reference := sdkgo.ConnectionRef{Provider: "snowflake", Name: record.connectionName}
		client, err := New(config, sdkgo.StaticCredentialProvider[Credentials]{reference: credentials})
		require.NoError(t, err, record.connectionName)
		_, err = NewConnection(client, reference)
		require.NoError(t, err, record.connectionName)
	}

	for _, contents := range []string{
		`{}`,
		`{"auth_method":"key-pair","private_key":"private-key-sentinel"}`,
		`{"auth_method":"programmatic-access-token"}`,
		`{"auth_method":"key-pair","user":"DEX","private_key":"k","password":"unexpected"}`,
	} {
		_, err := decodeCredentials(json.RawMessage(contents))
		require.Error(t, err, contents)
	}
}
