// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	responserecorder "github.com/superdurable/dex-connectors-library/connectors/google/forms/examples/response-recorder/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
)

func TestEnvironmentOr(t *testing.T) {
	t.Setenv("GOOGLE_FORMS_EXAMPLE_VALUE", "configured")
	require.Equal(t, "configured", environmentOr("GOOGLE_FORMS_EXAMPLE_VALUE", "fallback"))
	require.Equal(t, "fallback", environmentOr("GOOGLE_FORMS_EXAMPLE_MISSING", "fallback"))
}

func TestLoadFormConfigurationTreatsAMissingPickAsBlank(t *testing.T) {
	store := writeConnectionFiles(t, "")
	loaded, err := loadFormConfiguration(store, responserecorder.FormConfigurationRef())
	require.NoError(t, err)
	require.Equal(t, responserecorder.FormConfigurationRef(), loaded.Reference)
	require.Empty(t, loaded.Value.FormID)
}

func TestLoadFormConfigurationReadsTheSavedPick(t *testing.T) {
	store := writeConnectionFiles(t, `{"schemaVersion":"`+localconfig.UseConfigurationsSchemaVersion+`","operationConfigurations":[`+
		`{"connectorId":"google-forms","connectionName":"google-forms-intake","operationId":"getForm",`+
		`"flowType":"GoogleFormsResponseRecorder","stepType":"ReadForm","configuration":{"formId":"1FAIpQLintake","formTitle":"Vendor intake"}}]}`)
	loaded, err := loadFormConfiguration(store, responserecorder.FormConfigurationRef())
	require.NoError(t, err)
	require.Equal(t, responserecorder.FormConfiguration{FormID: "1FAIpQLintake", FormTitle: "Vendor intake"}, loaded.Value)
}

// writeConnectionFiles writes a connection file with the example's connection and, when useConfigurations
// is set, its sidecar.
func writeConnectionFiles(t *testing.T, useConfigurations string) *localconfig.Store {
	t.Helper()
	directory := t.TempDir()
	connectionsPath := filepath.Join(directory, "connections.json")
	require.NoError(t, os.WriteFile(connectionsPath, []byte(`{"schemaVersion":"connectors.dex.dev/local-connections/v1alpha1","connections":[`+
		`{"connectorId":"google-forms","modulePath":"github.com/superdurable/dex-connectors-library/connectors/google/forms","moduleVersion":"v0.1.0",`+
		`"provider":"google","connectionName":"`+responserecorder.ConnectionName+`","configuration":{},`+
		`"credentials":{"auth_method":"google-oauth","oauth_client_id":"client.apps.googleusercontent.com","oauth_client_secret":"secret",`+
		`"access_token":"access","refresh_token":"refresh"}}]}`), 0o600))
	if useConfigurations != "" {
		require.NoError(t, os.WriteFile(filepath.Join(directory, localconfig.UseConfigurationsFileName), []byte(useConfigurations), 0o600))
	}
	store, err := localconfig.LoadFile(connectionsPath)
	require.NoError(t, err)
	return store
}
