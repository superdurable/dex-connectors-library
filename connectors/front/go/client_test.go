// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package front_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/front"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestNewValidatesConfigurationAndOptions(t *testing.T) {
	_, err := front.New(front.Config{MaxResponseBytes: -1}, testCredentialProvider())
	require.Error(t, err)
	_, err = front.New(front.Config{}, nil)
	require.Error(t, err)
	_, err = front.New(front.Config{}, testCredentialProvider(), nil)
	require.Error(t, err)
	_, err = front.New(front.Config{}, testCredentialProvider(), front.WithClock(nil))
	require.Error(t, err)
	for _, baseURL := range []string{"http://api2.frontapp.com", "https://user:pass@api2.frontapp.com", "https://api2.frontapp.com?x=1", "ftp://127.0.0.1"} {
		_, err = front.New(front.Config{}, testCredentialProvider(), front.WithAPIBaseURL(baseURL))
		require.Error(t, err, baseURL)
	}
	client, err := front.New(front.Config{}, testCredentialProvider())
	require.NoError(t, err)
	require.NotNil(t, client)
	require.Equal(t, int64(4194304), front.DefaultConfig().MaxResponseBytes)
}

func TestConnectionNeverSerializesItsClient(t *testing.T) {
	connection, err := front.NewConnection(newFrontClient(t, "http://127.0.0.1:9"), frontConnection)
	require.NoError(t, err)
	_, err = connection.MarshalJSON()
	require.Error(t, err)
	require.Equal(t, "front.Connection{[REDACTED]}", fmt.Sprintf("%v", connection))
	_, err = front.NewConnection(nil, frontConnection)
	require.Error(t, err)
}

func TestTheAPITokenNeverFollowsARedirect(t *testing.T) {
	elsewhere := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("the token followed a redirect") }))
	t.Cleanup(elsewhere.Close)
	provider := newRecordingFront(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		http.Redirect(response, request, elsewhere.URL+"/contacts/crd_1", http.StatusTemporaryRedirect)
	})
	result, err := sdkgo.RunQuery(newTestDexContext("redirect"), newFrontClient(t, provider.URL).FindContactByEmail(), frontConnection,
		front.FindContactByEmailInput{Email: "jane@acme.example.com"})
	require.NoError(t, err)
	require.Equal(t, front.FindContactByEmailBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureProtocol, result.Failure.Kind)
}

func TestAnOversizedResponseSelectsInvalidResponse(t *testing.T) {
	provider := newRecordingFront(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"id":"crd_1","name":"`+string(make([]byte, 2048))+`"}`)
	})
	client, err := front.New(front.Config{MaxResponseBytes: 1024}, testCredentialProvider(), front.WithAPIBaseURL(provider.URL))
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(newTestDexContext("oversized"), client.FindContactByEmail(), frontConnection,
		front.FindContactByEmailInput{Email: "jane@acme.example.com"})
	require.NoError(t, err)
	require.Equal(t, front.FindContactByEmailBranchInvalidResponse, result.Branch)
	require.Equal(t, sdkgo.FailureResponseTooLarge, result.Failure.Kind)
}

func TestARateLimitWithoutRetryAfterWaitsForTheWindowReset(t *testing.T) {
	now := time.Unix(1767225600, 0)
	for name, test := range map[string]struct {
		header map[string]string
		delay  time.Duration
	}{
		"retry after":  {map[string]string{"Retry-After": "44"}, 44 * time.Second},
		"window reset": {map[string]string{"X-Ratelimit-Reset": fmt.Sprint(now.Unix() + 12)}, 12 * time.Second},
		"past reset":   {map[string]string{"X-Ratelimit-Reset": fmt.Sprint(now.Unix() - 5)}, time.Second},
		"no header":    {nil, 0},
	} {
		t.Run(name, func(t *testing.T) {
			provider := newRecordingFront(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				for key, value := range test.header {
					response.Header().Set(key, value)
				}
				writeFrontError(t, response, http.StatusTooManyRequests)
			})
			client := newFrontClient(t, provider.URL, front.WithClock(func() time.Time { return now }))
			_, err := sdkgo.RunQuery(newTestDexContext("rate-"+name), client.FindContactByEmail(), frontConnection,
				front.FindContactByEmailInput{Email: "jane@acme.example.com"})
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
			var retryAfter *dex.RetryAfterError
			if test.delay == 0 {
				require.NotErrorAs(t, err, &retryAfter, "the Step retry policy chooses the delay")
				return
			}
			require.ErrorAs(t, err, &retryAfter)
			require.Equal(t, test.delay, retryAfter.After)
		})
	}
}

func TestAMissingCredentialSelectsDefectWithoutARequest(t *testing.T) {
	provider := newRecordingFront(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request may be sent") })
	for name, credentials := range map[string]sdkgo.StaticCredentialProvider[front.Credentials]{
		"no connection": {},
		"header-unsafe": {frontConnection: {APIToken: sdkgo.NewSecretString("token with space")}},
	} {
		client, err := front.New(front.Config{}, credentials, front.WithAPIBaseURL(provider.URL))
		require.NoError(t, err)
		result, err := sdkgo.RunQuery(newTestDexContext("credentials-"+name), client.FindContactByEmail(), frontConnection,
			front.FindContactByEmailInput{Email: "jane@acme.example.com"})
		require.NoError(t, err)
		require.Equal(t, front.FindContactByEmailBranchDefect, result.Branch, name)
		require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
	}
}
