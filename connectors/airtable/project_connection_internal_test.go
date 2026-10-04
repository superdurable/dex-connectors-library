// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package airtable

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

// TestProjectConnectionReadsTheRecordsDexWebWrites decodes the records Dex Web saves, as NewProjectConnection does.
func TestProjectConnectionReadsTheRecordsDexWebWrites(t *testing.T) {
	key := projectconfig.ConnectionKey{ConnectorID: ConnectorID, ConnectionName: "airtable-refund-policies"}
	configuration := projectconfig.Configuration{Connections: []projectconfig.ConnectionConfiguration{{
		ConnectorID: ConnectorID, ConnectionName: key.ConnectionName,
		ModulePath: "github.com/superdurable/dex-connectors-library/connectors/airtable", Provider: "airtable",
		Configuration: json.RawMessage(`{}`),
	}}}
	var config Config
	require.NoError(t, configuration.DecodeConnectionConfiguration(key, &config))
	credentials, err := decodeCredentials(json.RawMessage(`{"personal_access_token":"patTESTtoken.0123456789abcdef"}`))
	require.NoError(t, err)
	reference := sdkgo.ConnectionRef{Provider: "airtable", Name: key.ConnectionName}
	client, err := New(config, sdkgo.StaticCredentialProvider[Credentials]{reference: credentials})
	require.NoError(t, err)
	_, err = NewConnection(client, reference)
	require.NoError(t, err)

	missing := projectconfig.ConnectionKey{ConnectorID: ConnectorID, ConnectionName: "missing"}
	require.ErrorIs(t, configuration.DecodeConnectionConfiguration(missing, &config), projectconfig.ErrObjectNotFound)
	for _, contents := range []string{`{}`, `{"personal_access_token":""}`, `{"personal_access_token":"x","refresh_token":"y"}`} {
		_, err := decodeCredentials(json.RawMessage(contents))
		require.Error(t, err, contents)
	}
}
