// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package salesforce_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/salesforce"
	"github.com/superdurable/dex-connectors-library/connectors/salesforce/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
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

type refreshingSalesforceConnection struct {
	credentials *testsupport.RefreshingCredentialSource[salesforce.Credentials]
	client      *salesforce.Client
	transport   *tokenEndpointTransport
}

func newRefreshingSalesforceConnection(t *testing.T, instanceURL string, accessToken string, respond func() (int, map[string]any)) *refreshingSalesforceConnection {
	t.Helper()
	transport := &tokenEndpointTransport{respond: respond}
	connection := &refreshingSalesforceConnection{transport: transport, credentials: testsupport.NewRefreshingCredentialSource(salesforce.Credentials{
		AuthMethodID: salesforce.ProductionOAuthAuthMethodID, OAuthClientID: "consumer-key",
		OAuthClientSecret: sdkgo.NewSecretString("consumer-secret"), AccessToken: sdkgo.NewSecretString(accessToken),
		RefreshToken: sdkgo.NewSecretString("refresh-token"), InstanceURL: instanceURL,
	}, nil)}
	var err error
	connection.client, err = salesforce.New(salesforce.Config{}, connection.credentials, salesforce.WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	return connection
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
	connection := newRefreshingSalesforceConnection(t, fake.URL, "expired-session", freshSessionResponse(fake.URL))

	result, err := runQueryRecords(t, connection.client, salesforce.QueryRecordsInput{SOQL: "SELECT Id FROM Account"})
	require.NoError(t, err)
	require.Equal(t, salesforce.QueryRecordsBranchFound, result.Branch)
	require.Equal(t, int32(1), connection.transport.tokenRequests.Load())
	require.Len(t, fake.recorded(), 2)
	stored, expiresAt := connection.credentials.Current()
	require.Equal(t, "fresh-session", stored.AccessToken.Reveal())
	require.Equal(t, "refresh-token", stored.RefreshToken.Reveal())
	require.NotNil(t, expiresAt)
}

func TestSecondSessionRejectionIsTerminalWithoutARefreshLoop(t *testing.T) {
	fake := sessionCheckingSalesforce(t, "never-valid")
	connection := newRefreshingSalesforceConnection(t, fake.URL, "expired-session", freshSessionResponse(fake.URL))

	result, err := runQueryRecords(t, connection.client, salesforce.QueryRecordsInput{SOQL: "SELECT Id FROM Account"})
	require.NoError(t, err)
	require.Equal(t, salesforce.QueryRecordsBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
	require.Equal(t, int32(1), connection.transport.tokenRequests.Load())
	require.Len(t, fake.recorded(), 2)
}

func TestRevokedRefreshTokenRequiresReauthorization(t *testing.T) {
	fake := sessionCheckingSalesforce(t, "fresh-session")
	connection := newRefreshingSalesforceConnection(t, fake.URL, "expired-session", func() (int, map[string]any) {
		return http.StatusBadRequest, map[string]any{"error": "invalid_grant", "error_description": "expired access/refresh token"}
	})

	rejected, err := runQueryRecords(t, connection.client, salesforce.QueryRecordsInput{SOQL: "SELECT Id FROM Account"})
	require.NoError(t, err)
	require.Equal(t, salesforce.QueryRecordsBranchProviderRejected, rejected.Branch)
	require.True(t, connection.credentials.IsReauthorizationRequired())

	stopped, err := runQueryRecords(t, connection.client, salesforce.QueryRecordsInput{SOQL: "SELECT Id FROM Account"})
	require.NoError(t, err)
	require.Equal(t, salesforce.QueryRecordsBranchProviderRejected, stopped.Branch)
	require.Contains(t, stopped.Failure.Message, "reauthorize")
	require.Len(t, fake.recorded(), 1)
	require.Equal(t, int32(1), connection.transport.tokenRequests.Load())
}

func TestMissingInstanceURLRefreshesBeforeTheFirstRequest(t *testing.T) {
	fake := sessionCheckingSalesforce(t, "fresh-session")
	connection := newRefreshingSalesforceConnection(t, "", "authorized-session", freshSessionResponse(fake.URL))

	result, err := runQueryRecords(t, connection.client, salesforce.QueryRecordsInput{SOQL: "SELECT Id FROM Account"})
	require.NoError(t, err)
	require.Equal(t, salesforce.QueryRecordsBranchFound, result.Branch)
	require.Len(t, fake.recorded(), 1)
	stored, _ := connection.credentials.Current()
	require.Equal(t, fake.URL, stored.InstanceURL)
}
