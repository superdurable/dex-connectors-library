// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package onedrive_test

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/onedrive"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/onedrive/internal/graphfake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
)

// tokenRoutingTransport answers login.microsoftonline.com itself and sends every other request to the network.
type tokenRoutingTransport struct {
	tokenRequests atomic.Int32
	accessToken   string
}

func (transport *tokenRoutingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Host != "login.microsoftonline.com" {
		return http.DefaultTransport.RoundTrip(request)
	}
	transport.tokenRequests.Add(1)
	if err := request.ParseForm(); err != nil || request.URL.Path != "/contoso.onmicrosoft.com/oauth2/v2.0/token" || request.Form.Get("grant_type") != "client_credentials" {
		return &http.Response{StatusCode: http.StatusBadRequest, Body: io.NopCloser(strings.NewReader(`{"error":"invalid_request"}`))}, nil
	}
	body := `{"access_token":"` + transport.accessToken + `","token_type":"Bearer","expires_in":3599}`
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
}

// TestLocalAppOnlyConnectionRequestsAndStoresATokenOnFirstUse starts from the record Dex Web saves:
// the method selection, the tenant, client ID, and secret, and no access token or expiry.
func TestLocalAppOnlyConnectionRequestsAndStoresATokenOnFirstUse(t *testing.T) {
	graph := newGraph(t)
	fileID := graph.AddItem(graphfake.Item{Name: "notes.txt", DriveID: teamDriveID, ParentID: "root", MimeType: "text/plain", Content: []byte("x")})
	configPath := writeAppOnlyConnectionFile(t, graph.URL)
	store, err := localconfig.LoadFile(configPath)
	require.NoError(t, err)
	transport := &tokenRoutingTransport{accessToken: graphTestToken}
	connection, err := onedrive.NewLocalConnection(store, graphConnection.Name, onedrive.WithHTTPClient(&http.Client{Transport: transport, Timeout: 10 * time.Second}))
	require.NoError(t, err)
	require.Equal(t, "onedrive.Connection{[REDACTED]}", connection.String())

	step := onedrive.NewGetFileStep(onedrive.GetFileStepConfig[string]{
		StepType: "Read", Annotations: testAnnotations, Connection: connection, ConnectionName: graphConnection.Name,
		MapToOperationInput: func(itemID string) onedrive.GetFileInput {
			return onedrive.GetFileInput{DriveID: teamDriveID, ItemID: itemID}
		},
		Found: sdkgo.GoTo(getTarget{}),
	})
	for range 2 {
		decision, err := step.Execute(newDexContext("local-app-only"), fileID)
		require.NoError(t, err)
		require.NotNil(t, decision)
	}
	require.Equal(t, int32(1), transport.tokenRequests.Load(), "the stored token is reused until it nears expiry")

	var file struct {
		Connections []struct {
			AuthMethodID        string            `json:"authMethodId"`
			Credentials         map[string]string `json:"credentials"`
			CredentialExpiresAt time.Time         `json:"credentialExpiresAt"`
		} `json:"connections"`
	}
	contents, err := os.ReadFile(configPath)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(contents, &file))
	record := file.Connections[0]
	require.Equal(t, onedrive.MicrosoftAppOnlyAuthMethodID, record.AuthMethodID, "Dex Web's record member survives the refresh")
	require.Equal(t, graphTestToken, record.Credentials["access_token"])
	require.Equal(t, "client-secret-value", record.Credentials["app_client_secret"])
	require.WithinDuration(t, time.Now().Add(time.Hour), record.CredentialExpiresAt, 2*time.Minute)
}

func writeAppOnlyConnectionFile(t *testing.T, endpoint string) string {
	t.Helper()
	record := map[string]any{
		"connectorId": onedrive.ConnectorID, "modulePath": "github.com/superdurable/dex-connectors-library/connectors/microsoft/onedrive",
		"moduleVersion": "v0.1.0", "provider": "microsoft", "connectionName": graphConnection.Name,
		"authMethodId":  onedrive.MicrosoftAppOnlyAuthMethodID,
		"configuration": map[string]any{"endpoint": endpoint},
		"credentials": map[string]any{
			"auth_method": onedrive.MicrosoftAppOnlyAuthMethodID, "tenant_id": "contoso.onmicrosoft.com",
			"app_client_id": "00000000-0000-0000-0000-0000000000bb", "app_client_secret": "client-secret-value",
		},
	}
	contents, err := json.Marshal(map[string]any{"schemaVersion": localconfig.SchemaVersion, "connections": []any{record}})
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "connections.json")
	require.NoError(t, os.WriteFile(path, contents, 0o600))
	return path
}
