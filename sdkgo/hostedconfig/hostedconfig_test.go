// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hostedconfig

import (
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

func TestCredentialProviderResolvesOperationScopedCredentialAndReloadsToken(t *testing.T) {
	tokenFile := writePrivateToken(t, "first-workload-token")
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
		BrokerURL: server.URL, WorkloadTokenFile: tokenFile,
		ConnectorID: "gmail", ConnectionName: "event-tickets",
	}, decodeTestCredentials)
	require.NoError(t, err)
	call := testCall("sendMessage")

	credentials, err := provider.Resolve(call)
	require.NoError(t, err)
	require.Equal(t, "short-lived", credentials.AccessToken.Reveal())
	require.NoError(t, os.WriteFile(tokenFile, []byte("second-workload-token\n"), 0o600))
	_, err = provider.Resolve(call)
	require.NoError(t, err)
	require.Equal(t, []string{"Bearer first-workload-token", "Bearer second-workload-token"}, observedTokens)
}

func TestCredentialProviderClassifiesReauthorizationWithoutReturningProviderText(t *testing.T) {
	tokenFile := writePrivateToken(t, "valid-workload-token")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusConflict)
		_, err := response.Write([]byte(`{"code":"reauthorization_required","message":"secret provider text"}`))
		require.NoError(t, err)
	}))
	t.Cleanup(server.Close)
	provider, err := NewCredentialProvider(&Config{
		BrokerURL: server.URL, WorkloadTokenFile: tokenFile,
		ConnectorID: "gmail", ConnectionName: "event-tickets",
	}, decodeTestCredentials)
	require.NoError(t, err)

	_, err = provider.Resolve(testCall("sendMessage"))
	require.Error(t, err)
	require.True(t, sdkgo.IsReauthorizationRequired(err))
	require.NotContains(t, err.Error(), "secret provider text")
}

func TestCredentialProviderRejectsMismatchedOperationAndUnsafeTokenFile(t *testing.T) {
	tokenFile := writePrivateToken(t, "valid-workload-token")
	provider, err := NewCredentialProvider(&Config{
		BrokerURL: "https://broker.example.test", WorkloadTokenFile: tokenFile,
		ConnectorID: "gmail", ConnectionName: "event-tickets",
	}, decodeTestCredentials)
	require.NoError(t, err)
	call := testCall("sendMessage")
	call.Operation.ConnectorID = "stripe"
	_, err = provider.Resolve(call)
	require.ErrorContains(t, err, "does not match")

	require.NoError(t, os.Chmod(tokenFile, 0o644))
	_, err = provider.Resolve(testCall("sendMessage"))
	require.ErrorContains(t, err, "private regular file")
}

func TestCredentialProviderRejectsOversizedOrUnknownResponses(t *testing.T) {
	tokenFile := writePrivateToken(t, "valid-workload-token")
	responses := []string{`{"credentials":{"access_token":"token"},"unknown":true}`, `{"credentials":"0123456789"}`}
	for _, responseBody := range responses {
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			_, err := response.Write([]byte(responseBody))
			require.NoError(t, err)
		}))
		provider, err := NewCredentialProvider(&Config{
			BrokerURL: server.URL, WorkloadTokenFile: tokenFile, ConnectorID: "gmail",
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

func writePrivateToken(t *testing.T, token string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "workload-token")
	require.NoError(t, os.WriteFile(path, []byte(token+"\n"), 0o600))
	return path
}
