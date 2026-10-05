// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pipedrive_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/pipedrive"
	"github.com/superdurable/dex-connectors-library/connectors/pipedrive/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestErrorResponsesSelectBranchesWithoutProviderText(t *testing.T) {
	for _, test := range []struct {
		name         string
		status       int
		header       map[string]string
		branch       sdkgo.BranchID
		failureKind  sdkgo.FailureKind
		isRetry      bool
		minimumDelay time.Duration
	}{
		{name: "invalid request", status: http.StatusBadRequest, branch: pipedrive.GetObjectBranchProviderRejected, failureKind: sdkgo.FailureValidation},
		{name: "unprocessable", status: http.StatusUnprocessableEntity, branch: pipedrive.GetObjectBranchProviderRejected, failureKind: sdkgo.FailureValidation},
		{name: "rejected token", status: http.StatusUnauthorized, branch: pipedrive.GetObjectBranchProviderRejected, failureKind: sdkgo.FailureAuthentication},
		{name: "payment required", status: http.StatusPaymentRequired, branch: pipedrive.GetObjectBranchProviderRejected, failureKind: sdkgo.FailureProviderRejection},
		{name: "forbidden", status: http.StatusForbidden, branch: pipedrive.GetObjectBranchProviderRejected, failureKind: sdkgo.FailureAuthorization},
		{name: "missing record", status: http.StatusNotFound, branch: pipedrive.GetObjectBranchNotFound, failureKind: sdkgo.FailureNotFound},
		{name: "gone endpoint", status: http.StatusGone, branch: pipedrive.GetObjectBranchProviderRejected, failureKind: sdkgo.FailureProviderRejection},
		{name: "not implemented", status: http.StatusNotImplemented, branch: pipedrive.GetObjectBranchProviderRejected, failureKind: sdkgo.FailureProviderRejection},
		{
			name: "daily token budget exhausted", status: http.StatusTooManyRequests, header: map[string]string{"X-Daily-Ratelimit-Token-Remaining": "0"},
			branch: pipedrive.GetObjectBranchProviderRejected, failureKind: sdkgo.FailureQuotaExhausted,
		},
		{name: "burst limit", status: http.StatusTooManyRequests, isRetry: true, failureKind: sdkgo.FailureRateLimit, minimumDelay: 2 * time.Second},
		{
			name: "burst limit with reset", status: http.StatusTooManyRequests, header: map[string]string{"X-Ratelimit-Reset": "5", "X-Daily-Ratelimit-Token-Remaining": "12"},
			isRetry: true, failureKind: sdkgo.FailureRateLimit, minimumDelay: 5 * time.Second,
		},
		{name: "server error", status: http.StatusInternalServerError, isRetry: true, failureKind: sdkgo.FailureAvailability},
		{name: "maintenance", status: http.StatusServiceUnavailable, header: map[string]string{"Retry-After": "7"}, isRetry: true, failureKind: sdkgo.FailureAvailability, minimumDelay: 7 * time.Second},
		{name: "request timeout", status: http.StatusRequestTimeout, isRetry: true, failureKind: sdkgo.FailureAvailability},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingPipedrive(t, func(response http.ResponseWriter, _ recordedRequest) {
				for name, value := range test.header {
					response.Header().Set(name, value)
				}
				writeError(t, response, test.status)
			})
			result, err := sdkgo.RunQuery(newStepDexContext("classify"), newAPITokenClient(t, provider.URL).GetObject(), pipedriveConnection, pipedrive.GetObjectInput{
				ObjectType: pipedrive.ObjectTypePersons, ObjectID: "9",
			})
			if test.isRetry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, test.failureKind, retry.Failure.Kind)
				require.NotContains(t, retry.Failure.Message, providerMessageSentinel)
				if test.minimumDelay > 0 {
					var retryAfter *dex.RetryAfterError
					require.ErrorAs(t, err, &retryAfter)
					require.GreaterOrEqual(t, retryAfter.After, test.minimumDelay)
				}
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			require.Equal(t, test.failureKind, result.Failure.Kind)
			require.NotContains(t, result.Failure.Message, providerMessageSentinel)
			require.Equal(t, testCorrelationID, result.Receipt.ProviderRequestID)
		})
	}
}

func TestResponsesThatEchoTheCredentialOrExceedTheLimitAreNeverKept(t *testing.T) {
	provider := newRecordingPipedrive(t, func(response http.ResponseWriter, request recordedRequest) {
		if request.path == "/api/v2/persons/1" {
			writeRecord(t, response, `{"id":1,"name":"`+testAPIToken+`"}`)
			return
		}
		writeRecord(t, response, `{"id":2,"name":"`+strings.Repeat("x", 2048)+`"}`)
	})
	client, err := pipedrive.New(pipedrive.Config{Endpoint: provider.URL, MaxResponseBytes: 1024}, sdkgo.StaticCredentialProvider[pipedrive.Credentials]{
		pipedriveConnection: {AuthMethodID: pipedrive.APITokenAuthMethodID, APIToken: sdkgo.NewSecretString(testAPIToken)},
	})
	require.NoError(t, err)
	for objectID, failureKind := range map[string]sdkgo.FailureKind{"1": sdkgo.FailureProtocol, "2": sdkgo.FailureResponseTooLarge} {
		result, err := sdkgo.RunQuery(newStepDexContext("unsafe-"+objectID), client.GetObject(), pipedriveConnection, pipedrive.GetObjectInput{
			ObjectType: pipedrive.ObjectTypePersons, ObjectID: objectID,
		})
		require.NoError(t, err)
		require.Equal(t, pipedrive.GetObjectBranchInvalidResponse, result.Branch)
		require.Equal(t, failureKind, result.Failure.Kind)
		require.Empty(t, result.Value.Fields)
	}
}

func TestNewAcceptsOnlyPipedriveHostsForTheAPITokenEndpoint(t *testing.T) {
	provider := sdkgo.StaticCredentialProvider[pipedrive.Credentials]{}
	for _, endpoint := range []string{
		"http://api.pipedrive.com", "https://api.pipedrive.com.example.com", "https://example.com", "https://user:secret@api.pipedrive.com",
		"https://api.pipedrive.com/api/v2", "https://api.pipedrive.com?token=secret",
	} {
		_, err := pipedrive.New(pipedrive.Config{Endpoint: endpoint}, provider)
		require.Error(t, err, endpoint)
		require.NotContains(t, err.Error(), "secret")
	}
	for _, endpoint := range []string{"", "https://api.pipedrive.com", "https://acme.pipedrive.com/", "http://127.0.0.1:8080"} {
		_, err := pipedrive.New(pipedrive.Config{Endpoint: endpoint}, provider)
		require.NoError(t, err, endpoint)
	}
	_, err := pipedrive.New(pipedrive.Config{}, nil)
	require.Error(t, err)
	_, err = pipedrive.New(pipedrive.Config{}, provider, nil)
	require.Error(t, err)
}

func TestOAuthRequestsGoToTheCompanyAPIDomainWithABearerToken(t *testing.T) {
	provider := newRecordingPipedrive(t, func(response http.ResponseWriter, _ recordedRequest) {
		writeRecord(t, response, `{"id":9,"name":"Jane"}`)
	})
	expiresAt := time.Now().Add(time.Hour)
	credentials := testsupport.NewRefreshingCredentialSource(oauthCredentials(provider.URL), &expiresAt)
	client, err := pipedrive.New(pipedrive.Config{Endpoint: "https://api.pipedrive.com"}, credentials)
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(newStepDexContext("oauth-get"), client.GetObject(), pipedriveConnection, pipedrive.GetObjectInput{
		ObjectType: pipedrive.ObjectTypePersons, ObjectID: "9",
	})
	require.NoError(t, err)
	require.Equal(t, pipedrive.GetObjectBranchFound, result.Branch)
	request := provider.recordedRequests()[0]
	require.Equal(t, "Bearer "+testAccessToken, request.authorization)
	require.Empty(t, request.apiToken)
}

func TestOAuthRejectionRefreshesOnceAndResendsToTheRefreshedAPIDomain(t *testing.T) {
	var provider *recordingPipedrive
	provider = newRecordingPipedrive(t, func(response http.ResponseWriter, request recordedRequest) {
		if request.authorization != "Bearer refreshed-access-token" {
			writeError(t, response, http.StatusUnauthorized)
			return
		}
		writeRecord(t, response, `{"id":9,"name":"Jane"}`)
	})
	expiresAt := time.Now().Add(time.Hour)
	credentials := testsupport.NewRefreshingCredentialSource(oauthCredentials(provider.URL), &expiresAt)
	tokenRequests := 0
	client, err := pipedrive.New(pipedrive.Config{}, credentials, pipedrive.WithHTTPClient(&http.Client{Transport: tokenEndpointTransport{tokenHandler: func(response http.ResponseWriter, _ *http.Request) {
		tokenRequests++
		writeJSON(t, response, http.StatusOK, `{"access_token":"refreshed-access-token","token_type":"Bearer","expires_in":3599,
			"refresh_token":"stored-refresh-token","scope":"base,deals:full,contacts:full,users:read","api_domain":"`+provider.URL+`"}`)
	}}}))
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(newStepDexContext("oauth-rejected"), client.GetObject(), pipedriveConnection, pipedrive.GetObjectInput{
		ObjectType: pipedrive.ObjectTypePersons, ObjectID: "9",
	})
	require.NoError(t, err)
	require.Equal(t, pipedrive.GetObjectBranchFound, result.Branch)
	require.Equal(t, 1, tokenRequests)
	require.Len(t, provider.recordedRequests(), 2, "the rejected read is sent once more with the refreshed token")
	current, _ := credentials.Current()
	require.Equal(t, "refreshed-access-token", current.AccessToken.Reveal())
}

func TestOAuthConnectionWithoutAnAPIDomainRefreshesBeforeItsFirstCall(t *testing.T) {
	provider := newRecordingPipedrive(t, func(response http.ResponseWriter, _ recordedRequest) {
		writeRecord(t, response, `{"id":9,"name":"Jane"}`)
	})
	expiresAt := time.Now().Add(time.Hour)
	credentials := testsupport.NewRefreshingCredentialSource(oauthCredentials(""), &expiresAt)
	client, err := pipedrive.New(pipedrive.Config{}, credentials, pipedrive.WithHTTPClient(&http.Client{Transport: tokenEndpointTransport{tokenHandler: func(response http.ResponseWriter, _ *http.Request) {
		writeJSON(t, response, http.StatusOK, `{"access_token":"refreshed-access-token","token_type":"bearer","expires_in":3599,"api_domain":"`+provider.URL+`"}`)
	}}}))
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(newStepDexContext("oauth-domain"), client.GetObject(), pipedriveConnection, pipedrive.GetObjectInput{
		ObjectType: pipedrive.ObjectTypePersons, ObjectID: "9",
	})
	require.NoError(t, err)
	require.Equal(t, pipedrive.GetObjectBranchFound, result.Branch)
	current, _ := credentials.Current()
	require.Equal(t, provider.URL, current.APIDomain)
	require.Equal(t, "stored-refresh-token", current.RefreshToken.Reveal(), "an omitted refresh token keeps the prior one")
}

func TestRevokedGrantSelectsProviderRejectedWithoutCallingPipedrive(t *testing.T) {
	provider := newRecordingPipedrive(t, func(response http.ResponseWriter, _ recordedRequest) { writeRecord(t, response, `{"id":9}`) })
	expired := time.Now().Add(-time.Minute)
	credentials := testsupport.NewRefreshingCredentialSource(oauthCredentials(provider.URL), &expired)
	client, err := pipedrive.New(pipedrive.Config{}, credentials, pipedrive.WithHTTPClient(&http.Client{Transport: tokenEndpointTransport{tokenHandler: func(response http.ResponseWriter, _ *http.Request) {
		writeJSON(t, response, http.StatusBadRequest, `{"success":false,"message":"`+providerMessageSentinel+`","error":"invalid_grant"}`)
	}}}))
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(newStepDexContext("oauth-revoked"), client.GetObject(), pipedriveConnection, pipedrive.GetObjectInput{
		ObjectType: pipedrive.ObjectTypePersons, ObjectID: "9",
	})
	require.NoError(t, err)
	require.Equal(t, pipedrive.GetObjectBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
	require.True(t, credentials.IsReauthorizationRequired())
	require.Empty(t, provider.recordedRequests())
}
