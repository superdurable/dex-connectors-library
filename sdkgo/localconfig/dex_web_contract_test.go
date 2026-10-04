// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//go:build dexcompat

package localconfig_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
)

// DexWebConnectionRecordsEnvironment names the directory where the Dex Web compatibility test wrote its files.
const DexWebConnectionRecordsEnvironment = "DEX_CONNECTOR_COMPAT_CONNECTION_RECORDS"

type passThroughRefreshDriver struct{}

func (passThroughRefreshDriver) RefreshRequired(sdkgo.CredentialRefreshState[json.RawMessage]) bool {
	return true
}

func (passThroughRefreshDriver) Refresh(
	_ context.Context,
	state sdkgo.CredentialRefreshState[json.RawMessage],
) (sdkgo.CredentialRefreshResult[json.RawMessage], error) {
	return sdkgo.CredentialRefreshResult[json.RawMessage]{Credentials: state.Credentials, ExpiresAt: time.Now().Add(time.Hour).UTC()}, nil
}

// TestDexWebWrittenConnectionRecords loads and refreshes the files released Dex Web wrote.
func TestDexWebWrittenConnectionRecords(t *testing.T) {
	sourceDirectory := os.Getenv(DexWebConnectionRecordsEnvironment)
	if sourceDirectory == "" {
		t.Fatal(DexWebConnectionRecordsEnvironment + " is required; script/dex_compatibility.py sets it")
	}
	directory := t.TempDir()
	for _, name := range []string{"connections.json", localconfig.UseConfigurationsFileName} {
		contents, err := os.ReadFile(filepath.Join(sourceDirectory, name))
		require.NoError(t, err, "Dex Web must have written %s", name)
		require.NoError(t, os.WriteFile(filepath.Join(directory, name), contents, 0o600))
	}
	path := filepath.Join(directory, "connections.json")
	before := readRecordMembers(t, path)
	store, err := localconfig.LoadFile(path)
	require.NoError(t, err, "the SDK must load Dex Web's connections.json")

	var connections struct {
		Connections []struct {
			ConnectorID    string `json:"connectorId"`
			ConnectionName string `json:"connectionName"`
			Provider       string `json:"provider"`
		} `json:"connections"`
	}
	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(contents, &connections))
	require.NotEmpty(t, connections.Connections)
	passThrough := func(credentials json.RawMessage) (json.RawMessage, error) { return credentials, nil }
	for _, connection := range connections.Connections {
		provider := localconfig.NewRefreshingCredentialProvider(store, connection.ConnectorID, connection.ConnectionName, passThrough, passThrough)
		_, err := sdkgo.ResolveCredential(context.Background(), provider, sdkgo.Call{
			Connection: sdkgo.ConnectionRef{Provider: connection.Provider, Name: connection.ConnectionName},
		}, passThroughRefreshDriver{})
		require.NoError(t, err, "refresh %s/%s", connection.ConnectorID, connection.ConnectionName)
	}
	after := readRecordMembers(t, path)
	for identity, beforeMembers := range before {
		for name, beforeValue := range beforeMembers {
			if name == "credentialExpiresAt" || name == "credentialStatus" {
				continue
			}
			require.JSONEq(t, string(beforeValue), string(after[identity][name]), "%s member %s after refresh", identity, name)
		}
	}
	reloaded, err := localconfig.LoadFile(path)
	require.NoError(t, err, "the refreshed file must load again")

	var useConfigurations struct {
		OperationConfigurations []sdkgo.ConnectorConfigurationRef `json:"operationConfigurations"`
	}
	contents, err = os.ReadFile(filepath.Join(directory, localconfig.UseConfigurationsFileName))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(contents, &useConfigurations))
	require.NotEmpty(t, useConfigurations.OperationConfigurations)
	for _, reference := range useConfigurations.OperationConfigurations {
		_, err := localconfig.LoadOperationConfiguration[map[string]json.RawMessage](reloaded, reference)
		require.NoError(t, err, "load use configuration %+v", reference)
	}
}
