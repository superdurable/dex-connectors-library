// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package excel_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/excel"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/excel/internal/fakeexcel"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
)

const localConnectionName = "excel-approvals"

// writeLocalConnectionsFile writes the record shape Dex Web saves after Microsoft consent, without an expiry.
func writeLocalConnectionsFile(t *testing.T, path string, accessToken string, refreshToken string) {
	t.Helper()
	file := map[string]any{
		"schemaVersion": localconfig.SchemaVersion,
		"connections": []map[string]any{{
			"connectorId": excel.ConnectorID, "modulePath": "github.com/superdurable/dex-connectors-library/connectors/microsoft/excel",
			"moduleVersion": "v0.1.0", "provider": "microsoft", "connectionName": localConnectionName, "authMethodId": excel.OAuthAuthMethodID,
			"configuration": map[string]any{},
			"credentials": map[string]any{
				"auth_method": excel.OAuthAuthMethodID, "oauth_client_id": "00001111-aaaa-2222-bbbb-3333cccc4444",
				"oauth_client_secret": "client-secret", "access_token": accessToken, "refresh_token": refreshToken,
			},
		}},
	}
	encoded, err := json.Marshal(file)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, encoded, 0o600))
}

func TestLocalConnectionRefreshesBeforeTheFirstCallAndPersistsTheRotatedTokens(t *testing.T) {
	provider := fakeexcel.New(t, "unused-until-refresh", "consent-refresh")
	provider.AddWorksheet(testDriveID, testWorkbookID, "{00000000-0001-0000-0000-000000000000}", "Summary")
	path := filepath.Join(t.TempDir(), "connections.json")
	writeLocalConnectionsFile(t, path, "consent-access", "consent-refresh")
	store, err := localconfig.LoadFile(path)
	require.NoError(t, err)
	connection, err := excel.NewLocalConnection(store, localConnectionName, excel.WithLocalProviderURL(provider.URL))
	require.NoError(t, err)

	client, reference := excel.ConnectionInternalsForTest(connection)
	result, err := sdkgo.RunQuery(newDexContext("local"), client.GetValues(), reference, excel.GetValuesInput{
		DriveID: testDriveID, WorkbookID: testWorkbookID, Worksheet: "Summary", Address: "A1",
	})
	require.NoError(t, err)
	require.Equal(t, excel.GetValuesBranchRead, result.Branch)
	require.Equal(t, 1, provider.Count(fakeexcel.CountTokenRefreshes), "a record without an expiry is refreshed once before use")
	require.Zero(t, provider.Count(fakeexcel.CountUnauthorized))

	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	var persisted struct {
		Connections []struct {
			AuthMethodID string `json:"authMethodId"`
			Credentials  struct {
				AccessToken  string `json:"access_token"`
				RefreshToken string `json:"refresh_token"`
			} `json:"credentials"`
			CredentialExpiresAt time.Time `json:"credentialExpiresAt"`
		} `json:"connections"`
	}
	require.NoError(t, json.Unmarshal(contents, &persisted))
	record := persisted.Connections[0]
	require.Equal(t, provider.AccessToken(), record.Credentials.AccessToken)
	require.Equal(t, "fake-excel-refresh-1", record.Credentials.RefreshToken, "the rotated refresh token replaces the consent one")
	require.Equal(t, excel.OAuthAuthMethodID, record.AuthMethodID, "Dex Web's record member survives the rewrite")
	require.True(t, record.CredentialExpiresAt.After(time.Now().Add(50*time.Minute)))
}
