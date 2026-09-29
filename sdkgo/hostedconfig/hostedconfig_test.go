// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hostedconfig

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/internal/testsupport"
)

type testCredentials struct {
	AccessToken sdkgo.SecretString
}

type unusedRefreshDriver struct{}

func (unusedRefreshDriver) RefreshRequired(sdkgo.CredentialRefreshState[testCredentials]) bool {
	return true
}

func (unusedRefreshDriver) Refresh(
	context.Context,
	sdkgo.CredentialRefreshState[testCredentials],
) (sdkgo.CredentialRefreshResult[testCredentials], error) {
	return sdkgo.CredentialRefreshResult[testCredentials]{}, nil
}

func TestCredentialProviderResolvesOperationScopedCredentialAndReloadsWorkloadCredential(t *testing.T) {
	credentialFile := writePrivateCredential(t, "first-workload-credential")
	var observedTokens []string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		observedTokens = append(observedTokens, request.Header.Get("Authorization"))
		var body resolveRequest
		require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
		require.Equal(t, "gmail", body.ConnectorID)
		require.Equal(t, "event-tickets", body.ConnectionName)
		require.Equal(t, "sendMessage", body.OperationID)
		response.Header().Set("Content-Type", "application/json")
		_, err := response.Write([]byte(`{"credentials":{"access_token":"short-lived"}}`))
		require.NoError(t, err)
	}))
	t.Cleanup(server.Close)
	provider, err := NewCredentialProvider(&Config{
		BrokerURL: server.URL, WorkloadCredentialFile: credentialFile,
		ConnectorID: "gmail", ConnectionName: "event-tickets",
	}, decodeTestCredentials)
	require.NoError(t, err)
	call := testCall("sendMessage")

	credentials, err := provider.Resolve(call)
	require.NoError(t, err)
	require.Equal(t, "short-lived", credentials.AccessToken.Reveal())
	require.NoError(t, os.WriteFile(credentialFile, []byte("second-workload-credential\n"), 0o600))
	_, err = provider.Resolve(call)
	require.NoError(t, err)
	require.Equal(t, []string{"Bearer first-workload-credential", "Bearer second-workload-credential"}, observedTokens)
}

func TestCredentialProviderResolvesTriggerCredentialWithoutDexContext(t *testing.T) {
	credentialFile := writePrivateCredential(t, "trigger-workload-credential")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, "Bearer trigger-workload-credential", request.Header.Get("Authorization"))
		var body resolveRequest
		require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
		require.Equal(t, "messageReceived", body.OperationID)
		response.Header().Set("Content-Type", "application/json")
		_, err := response.Write([]byte(`{"credentials":{"access_token":"trigger-token"}}`))
		require.NoError(t, err)
	}))
	t.Cleanup(server.Close)
	provider, err := NewCredentialProvider(&Config{
		BrokerURL: server.URL, WorkloadCredentialFile: credentialFile,
		ConnectorID: "gmail", ConnectionName: "event-tickets",
	}, decodeTestCredentials)
	require.NoError(t, err)
	call := testCall("messageReceived")
	call.Context = nil

	credentials, err := provider.Resolve(call)
	require.NoError(t, err)
	require.Equal(t, "trigger-token", credentials.AccessToken.Reveal())
}

func TestCredentialProviderRequestsForcedRefreshAfterProviderRejection(t *testing.T) {
	credentialFile := writePrivateCredential(t, "valid-workload-credential")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var body resolveRequest
		require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
		require.True(t, body.ForceRefresh)
		_, err := response.Write([]byte(`{"credentials":{"access_token":"replacement-token"}}`))
		require.NoError(t, err)
	}))
	t.Cleanup(server.Close)
	provider, err := NewCredentialProvider(&Config{
		BrokerURL: server.URL, WorkloadCredentialFile: credentialFile,
		ConnectorID: "gmail", ConnectionName: "event-tickets",
	}, decodeTestCredentials)
	require.NoError(t, err)

	credentials, err := sdkgo.ResolveCredentialAfterRejection(
		context.Background(), provider, testCall("sendMessage"), unusedRefreshDriver{},
	)
	require.NoError(t, err)
	require.Equal(t, "replacement-token", credentials.AccessToken.Reveal())
}

func TestCredentialProviderClassifiesReauthorizationWithoutReturningProviderText(t *testing.T) {
	credentialFile := writePrivateCredential(t, "valid-workload-credential")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusConflict)
		_, err := response.Write([]byte(`{"code":"reauthorization_required","message":"secret provider text"}`))
		require.NoError(t, err)
	}))
	t.Cleanup(server.Close)
	provider, err := NewCredentialProvider(&Config{
		BrokerURL: server.URL, WorkloadCredentialFile: credentialFile,
		ConnectorID: "gmail", ConnectionName: "event-tickets",
	}, decodeTestCredentials)
	require.NoError(t, err)

	_, err = provider.Resolve(testCall("sendMessage"))
	require.Error(t, err)
	require.True(t, sdkgo.IsReauthorizationRequired(err))
	require.NotContains(t, err.Error(), "secret provider text")
}

func TestCredentialProviderRejectsMismatchedOperationAndUnsafeCredentialFile(t *testing.T) {
	credentialFile := writePrivateCredential(t, "valid-workload-credential")
	provider, err := NewCredentialProvider(&Config{
		BrokerURL: "https://broker.example.test", WorkloadCredentialFile: credentialFile,
		ConnectorID: "gmail", ConnectionName: "event-tickets",
	}, decodeTestCredentials)
	require.NoError(t, err)
	call := testCall("sendMessage")
	call.Operation.ConnectorID = "stripe"
	_, err = provider.Resolve(call)
	require.ErrorContains(t, err, "does not match")

	require.NoError(t, os.Chmod(credentialFile, 0o644))
	_, err = provider.Resolve(testCall("sendMessage"))
	require.ErrorContains(t, err, "private regular file")
}

func TestCredentialProviderRejectsOversizedOrUnknownResponses(t *testing.T) {
	credentialFile := writePrivateCredential(t, "valid-workload-credential")
	responses := []string{`{"credentials":{"access_token":"token"},"unknown":true}`, `{"credentials":"0123456789"}`}
	for _, responseBody := range responses {
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			_, err := response.Write([]byte(responseBody))
			require.NoError(t, err)
		}))
		provider, err := NewCredentialProvider(&Config{
			BrokerURL: server.URL, WorkloadCredentialFile: credentialFile, ConnectorID: "gmail",
			ConnectionName: "event-tickets", MaximumResponseBytes: 32,
		}, decodeTestCredentials)
		require.NoError(t, err)
		_, err = provider.Resolve(testCall("sendMessage"))
		require.Error(t, err)
		server.Close()
	}
}

func decodeTestCredentials(contents json.RawMessage) (testCredentials, error) {
	var fields struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(contents, &fields); err != nil {
		return testCredentials{}, err
	}
	return testCredentials{AccessToken: sdkgo.NewSecretString(fields.AccessToken)}, nil
}

func testCall(operationID string) sdkgo.Call {
	return sdkgo.Call{
		Context:    testsupport.NewDexContext("hosted-test", "resolve-credential"),
		ID:         "ec8caece-c546-5d3c-83d1-5dd20f811673",
		Connection: sdkgo.ConnectionRef{Provider: "google", Name: "event-tickets"},
		Operation:  sdkgo.OperationRef{ConnectorID: "gmail", OperationID: operationID},
	}
}

func writePrivateCredential(t *testing.T, credential string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "workload-credential")
	require.NoError(t, os.WriteFile(path, []byte(credential+"\n"), 0o600))
	return path
}
