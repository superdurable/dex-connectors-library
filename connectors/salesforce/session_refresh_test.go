// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package salesforce_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/salesforce"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
)

// tokenEndpointTransport answers login.salesforce.com token requests and forwards every other request.
type tokenEndpointTransport struct {
	respond       func() (int, map[string]any)
	tokenRequests atomic.Int32
}

func (transport *tokenEndpointTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Host != "login.salesforce.com" {
		return http.DefaultTransport.RoundTrip(request)
	}
	transport.tokenRequests.Add(1)
	status, body := transport.respond()
	contents, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(bytes.NewReader(contents))}, nil
}

type localSalesforceConnection struct {
	path      string
	client    *salesforce.Client
	transport *tokenEndpointTransport
}

func newLocalSalesforceConnection(t *testing.T, instanceURL string, accessToken string, respond func() (int, map[string]any)) *localSalesforceConnection {
	t.Helper()
	path := filepath.Join(t.TempDir(), "connections.json")
	contents, err := json.Marshal(map[string]any{
		"schemaVersion": localconfig.SchemaVersion,
		"connections": []map[string]any{{
			"connectorId": salesforce.ConnectorID, "connectionName": salesforceConnection.Name, "provider": "salesforce",
			"modulePath": "github.com/superdurable/dex-connectors-library/connectors/salesforce", "moduleVersion": "v0.1.0",
			"configuration": map[string]any{},
			"credentials": map[string]any{
				"auth_method": salesforce.ProductionOAuthAuthMethodID, "oauth_client_id": "consumer-key",
				"oauth_client_secret": "consumer-secret", "access_token": accessToken, "refresh_token": "refresh-token",
				"instance_url": instanceURL,
			},
		}},
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, contents, 0o600))
	store, err := localconfig.LoadFile(path)
	require.NoError(t, err)
	transport := &tokenEndpointTransport{respond: respond}
	connection := &localSalesforceConnection{path: path, transport: transport}
	credentials := localconfig.NewRefreshingCredentialProvider(store, salesforce.ConnectorID, salesforceConnection.Name,
		salesforce.DecodeCredentialsJSON, encodeCredentialsRawJSON)
	connection.client, err = salesforce.New(salesforce.Config{}, credentials, salesforce.WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	return connection
}

func encodeCredentialsRawJSON(credentials salesforce.Credentials) (json.RawMessage, error) {
	return salesforce.EncodeCredentialsJSON(credentials)
}

func (connection *localSalesforceConnection) storedRecord(t *testing.T) map[string]any {
	t.Helper()
	contents, err := os.ReadFile(connection.path)
	require.NoError(t, err)
	var file struct {
		Connections []map[string]any `json:"connections"`
	}
	require.NoError(t, json.Unmarshal(contents, &file))
	require.Len(t, file.Connections, 1)
	return file.Connections[0]
}

func freshSessionResponse(instanceURL string) func() (int, map[string]any) {
	return func() (int, map[string]any) {
		return http.StatusOK, map[string]any{"access_token": "fresh-session", "token_type": "Bearer", "scope": "api refresh_token", "instance_url": instanceURL}
	}
}

func sessionCheckingSalesforce(t *testing.T, validToken string) *fakeSalesforce {
	return newFakeSalesforce(t, func(response http.ResponseWriter, request *http.Request, _ []byte) {
		if request.Header.Get("Authorization") != "Bearer "+validToken {
			writeJSON(t, response, http.StatusUnauthorized, `[{"message":"Session expired or invalid","errorCode":"INVALID_SESSION_ID"}]`)
			return
		}
		writeJSON(t, response, http.StatusOK, `{"totalSize":1,"done":true,"records":[{"attributes":{"type":"Account"},"Id":"001RM000001AbcdYAC"}]}`)
	})
}

func TestRejectedSessionRefreshesOnceAndResendsOnce(t *testing.T) {
	fake := sessionCheckingSalesforce(t, "fresh-session")
	connection := newLocalSalesforceConnection(t, fake.URL, "expired-session", freshSessionResponse(fake.URL))

	result, err := runQueryRecords(t, connection.client, salesforce.QueryRecordsInput{SOQL: "SELECT Id FROM Account"})
	require.NoError(t, err)
	require.Equal(t, salesforce.QueryRecordsBranchFound, result.Branch)
	require.Equal(t, int32(1), connection.transport.tokenRequests.Load())
	require.Len(t, fake.recorded(), 2)
	stored := connection.storedRecord(t)
	require.Equal(t, "fresh-session", stored["credentials"].(map[string]any)["access_token"])
	require.Equal(t, "refresh-token", stored["credentials"].(map[string]any)["refresh_token"])
	require.NotEmpty(t, stored["credentialExpiresAt"])
}

func TestSecondSessionRejectionIsTerminalWithoutARefreshLoop(t *testing.T) {
	fake := sessionCheckingSalesforce(t, "never-valid")
	connection := newLocalSalesforceConnection(t, fake.URL, "expired-session", freshSessionResponse(fake.URL))

	result, err := runQueryRecords(t, connection.client, salesforce.QueryRecordsInput{SOQL: "SELECT Id FROM Account"})
	require.NoError(t, err)
	require.Equal(t, salesforce.QueryRecordsBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
	require.Equal(t, int32(1), connection.transport.tokenRequests.Load())
	require.Len(t, fake.recorded(), 2)
}

func TestRevokedRefreshTokenRequiresReauthorization(t *testing.T) {
	fake := sessionCheckingSalesforce(t, "fresh-session")
	connection := newLocalSalesforceConnection(t, fake.URL, "expired-session", func() (int, map[string]any) {
		return http.StatusBadRequest, map[string]any{"error": "invalid_grant", "error_description": "expired access/refresh token"}
	})

	rejected, err := runQueryRecords(t, connection.client, salesforce.QueryRecordsInput{SOQL: "SELECT Id FROM Account"})
	require.NoError(t, err)
	require.Equal(t, salesforce.QueryRecordsBranchProviderRejected, rejected.Branch)
	require.Equal(t, "reauthorization_required", connection.storedRecord(t)["credentialStatus"])

	stopped, err := runQueryRecords(t, connection.client, salesforce.QueryRecordsInput{SOQL: "SELECT Id FROM Account"})
	require.NoError(t, err)
	require.Equal(t, salesforce.QueryRecordsBranchProviderRejected, stopped.Branch)
	require.Contains(t, stopped.Failure.Message, "reauthorize")
	require.Len(t, fake.recorded(), 1)
	require.Equal(t, int32(1), connection.transport.tokenRequests.Load())
}

func TestMissingInstanceURLRefreshesBeforeTheFirstRequest(t *testing.T) {
	fake := sessionCheckingSalesforce(t, "fresh-session")
	connection := newLocalSalesforceConnection(t, "", "authorized-session", freshSessionResponse(fake.URL))

	result, err := runQueryRecords(t, connection.client, salesforce.QueryRecordsInput{SOQL: "SELECT Id FROM Account"})
	require.NoError(t, err)
	require.Equal(t, salesforce.QueryRecordsBranchFound, result.Branch)
	require.Len(t, fake.recorded(), 1)
	require.Equal(t, fake.URL, connection.storedRecord(t)["credentials"].(map[string]any)["instance_url"])
}
