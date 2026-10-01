// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/trello"
	approvedrequestcard "github.com/superdurable/dex-connectors-library/connectors/atlassian/trello/examples/approved-request-card/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
)

func TestEnvironmentOr(t *testing.T) {
	t.Setenv("TRELLO_EXAMPLE_VALUE", "configured")
	require.Equal(t, "configured", environmentOr("TRELLO_EXAMPLE_VALUE", "fallback"))
	require.Equal(t, "fallback", environmentOr("TRELLO_EXAMPLE_MISSING", "fallback"))
}

// TestDexWebConnectionRecordLoads proves the record Dex Web writes for this example's connection decodes,
// including Dex Web's authMethodId member.
func TestDexWebConnectionRecordLoads(t *testing.T) {
	connectionsPath := filepath.Join(t.TempDir(), "connections.json")
	require.NoError(t, os.WriteFile(connectionsPath, []byte(`{"schemaVersion":"connectors.dex.dev/local-connections/v1alpha1","connections":[{
		"connectorId":"trello","modulePath":"github.com/superdurable/dex-connectors-library/connectors/atlassian/trello",
		"moduleVersion":"v0.1.0","provider":"trello","connectionName":"`+approvedrequestcard.ConnectionName+`","authMethodId":"",
		"configuration":{},"credentials":{"api_key":"0123456789abcdef0123456789abcdef","token":"ATTAexampletoken"}}]}`), 0o600))
	store, err := localconfig.LoadFile(connectionsPath)
	require.NoError(t, err)
	connection, err := trello.NewLocalConnection(store, approvedrequestcard.ConnectionName)
	require.NoError(t, err)
	require.Equal(t, "trello.Connection{[REDACTED]}", connection.String())
}
