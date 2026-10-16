// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package discord

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

// TestProjectConnectionReadsTheRecordsDexWebWrites decodes the records Dex Web saves, as NewProjectConnection does.
func TestProjectConnectionReadsTheRecordsDexWebWrites(t *testing.T) {
	key := projectconfig.ConnectionKey{ConnectorID: ConnectorID, ConnectionName: "discord"}
	configuration := projectconfig.Configuration{Connections: []projectconfig.ConnectionConfiguration{{
		ConnectorID: ConnectorID, ConnectionName: key.ConnectionName,
		ModulePath: "github.com/superdurable/dex-connectors-library/connectors/discord", Provider: "discord",
		Configuration: json.RawMessage(`{}`),
	}}}
	var config Config
	require.NoError(t, configuration.DecodeConnectionConfiguration(key, &config))
	credentials, err := decodeCredentials(json.RawMessage(`{"bot_token":"MTAxTESTbot.token-part.0123456789abcdef"}`))
	require.NoError(t, err)
	reference := sdkgo.ConnectionRef{Provider: "discord", Name: key.ConnectionName}
	client, err := New(config, sdkgo.StaticCredentialProvider[Credentials]{reference: credentials})
	require.NoError(t, err)
	_, err = NewConnection(client, reference)
	require.NoError(t, err)
	require.Equal(t, "discord.Connection{[REDACTED]}", Connection{}.String())

	for _, contents := range []string{`{}`, `{"bot_token":""}`, `{"bot_token":"x","refresh_token":"y"}`} {
		_, err := decodeCredentials(json.RawMessage(contents))
		require.Error(t, err, contents)
	}
}

func TestMessageNonceFitsDiscordsLimitAndIsStable(t *testing.T) {
	nonce := messageNonce("4b1c5e0a-0000-5000-8000-000000000001")
	require.Len(t, nonce, 25)
	require.Equal(t, nonce, messageNonce("4b1c5e0a-0000-5000-8000-000000000001"))
	require.NotEqual(t, nonce, messageNonce("4b1c5e0a-0000-5000-8000-000000000002"))
}
