// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package confluence_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/confluence"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func policyPageJSON(t *testing.T) string {
	t.Helper()
	return pageJSON(t, testPolicyPageID, testPolicyTitle, 3, "Annual review", time.Date(2026, time.September, 1, 9, 0, 0, 0, time.UTC),
		"<h1>Remote work</h1><p>Two days a week.</p>")
}

func TestNewRejectsInvalidSitesEndpointsAndLimits(t *testing.T) {
	credentials := staticConfluenceCredentials()
	_, err := confluence.New(confluence.Config{CloudID: "your-site.atlassian.net"}, credentials)
	require.ErrorContains(t, err, "cloudId must be a site UUID")
	_, err = confluence.New(confluence.Config{Endpoint: "http://api.atlassian.com"}, credentials)
	require.ErrorContains(t, err, "HTTPS")
	_, err = confluence.New(confluence.Config{MaxResponseBytes: -1}, credentials)
	require.Error(t, err)
	_, err = confluence.New(confluence.Config{}, nil)
	require.ErrorContains(t, err, "credential provider is required")
	_, err = confluence.New(confluence.Config{}, credentials, nil)
	require.ErrorContains(t, err, "option is nil")
	client, err := confluence.New(confluence.Config{CloudID: " " + testCloudID + " "}, credentials)
	require.NoError(t, err)
	require.NotNil(t, client)
}

func TestRequestsUseTheSiteGatewayPathAndTheBearerToken(t *testing.T) {
	provider := newRecordingConfluence(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, policyPageJSON(t))
	})
	client := newConfluenceClient(t, provider.URL)

	result, err := sdkgo.RunQuery(newTestDexContext("site-path"), client.GetPage(), confluenceConnection, confluence.GetPageInput{PageID: testPolicyPageID})
	require.NoError(t, err)
	require.Equal(t, confluence.GetPageBranchFound, result.Branch)
	request := provider.request(0)
	require.Equal(t, http.MethodGet, request.method)
	require.Equal(t, testContentPath+"/pages/"+testPolicyPageID, request.path)
	require.Equal(t, "body-format=storage", request.rawQuery)
	require.Equal(t, "Bearer "+testAccessToken, request.authorization)
	require.Equal(t, "trace-1", result.Receipt.ProviderRequestID)
	require.Equal(t, testPolicyPageID, result.Receipt.ProviderObjectID)
}

func TestBlankCloudIDUsesTheOnlyGrantedConfluenceSiteOnce(t *testing.T) {
	provider := newRecordingConfluence(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.URL.Path == "/oauth/token/accessible-resources" {
			writeJSON(t, response, http.StatusOK, `[`+
				`{"id":"99999999-0000-4000-8000-000000000000","name":"Tracker","url":"https://track.atlassian.net","scopes":["read:jira-work"]},`+
				`{"id":"`+testCloudID+`","name":"Ops","url":"https://ops.atlassian.net","scopes":["search:confluence","read:page:confluence"]}]`)
			return
		}
		writeJSON(t, response, http.StatusOK, policyPageJSON(t))
	})
	client := newConfluenceClientForSite(t, provider.URL, "")

	for _, step := range []string{"first", "second"} {
		result, err := sdkgo.RunQuery(newTestDexContext(step), client.GetPage(), confluenceConnection, confluence.GetPageInput{PageID: testPolicyPageID})
		require.NoError(t, err)
		require.Equal(t, confluence.GetPageBranchFound, result.Branch)
	}
	require.Equal(t, 3, provider.requestCount(), "the site is looked up once and cached")
	require.Equal(t, "/oauth/token/accessible-resources", provider.request(0).path)
	require.Equal(t, testContentPath+"/pages/"+testPolicyPageID, provider.request(1).path)
}

func TestBlankCloudIDSelectsDefectUnlessExactlyOneConfluenceSiteIsGranted(t *testing.T) {
	for _, test := range []struct {
		name      string
		resources string
		kind      sdkgo.FailureKind
		message   string
	}{
		{name: "no Confluence site", resources: `[{"id":"` + testCloudID + `","scopes":["read:jira-work"]}]`,
			kind: sdkgo.FailureAuthorization, message: "the authorization grants no Confluence site; reconnect and pick a Confluence site on the consent screen"},
		{name: "several Confluence sites", resources: `[{"id":"` + testCloudID + `","scopes":["read:page:confluence"]},{"id":"99999999-0000-4000-8000-000000000000","scopes":["search:confluence"]}]`,
			kind: sdkgo.FailureValidation, message: "the authorization grants 2 Confluence sites; choose one with the Confluence site picker or set cloudId"},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingConfluence(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, http.StatusOK, test.resources)
			})
			client := newConfluenceClientForSite(t, provider.URL, "")
			result, err := sdkgo.RunQuery(newTestDexContext("site-"+test.name), client.GetPage(), confluenceConnection, confluence.GetPageInput{PageID: testPolicyPageID})
			require.NoError(t, err)
			require.Equal(t, confluence.GetPageBranchDefect, result.Branch)
			require.Equal(t, test.kind, result.Failure.Kind)
			require.Equal(t, test.message, result.Failure.Message)
			require.Equal(t, 1, provider.requestCount(), "no Confluence request is sent without a site")
		})
	}
}

func TestAccessibleResourcesOutageRetriesTheStep(t *testing.T) {
	provider := newRecordingConfluence(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusServiceUnavailable, `{"message":"SENTINEL outage"}`)
	})
	client := newConfluenceClientForSite(t, provider.URL, "")
	_, err := sdkgo.RunMutation(newTestDexContext("site-outage"), client.CreatePage(), confluenceConnection, validCreatePageInput())
	requireRetry(t, err, sdkgo.FailureAvailability)
	require.Equal(t, 1, provider.requestCount(), "no create is sent while the site is unknown")
}

func TestUnauthorizedRequestRefreshesOnceAndResendsWithTheReplacement(t *testing.T) {
	provider := newRecordingConfluence(t, func(response http.ResponseWriter, request *http.Request, index int) {
		if index == 0 {
			require.Equal(t, "Bearer rejected-token", request.Header.Get("Authorization"))
			writeJSON(t, response, http.StatusUnauthorized, `{"code":401,"message":"SENTINEL Unauthorized"}`)
			return
		}
		require.Equal(t, "Bearer replacement-token", request.Header.Get("Authorization"))
		writeJSON(t, response, http.StatusOK, policyPageJSON(t))
	})
	credentials := &rejectionRefreshingCredentialProvider{}
	client, err := confluence.New(confluence.Config{CloudID: testCloudID, Endpoint: provider.URL}, credentials)
	require.NoError(t, err)

	result, err := sdkgo.RunQuery(newTestDexContext("refresh-once"), client.GetPage(), confluenceConnection, confluence.GetPageInput{PageID: testPolicyPageID})
	require.NoError(t, err)
	require.Equal(t, confluence.GetPageBranchFound, result.Branch)
	require.Equal(t, 2, provider.requestCount())
	require.Equal(t, 1, credentials.forcedRefreshes)
}

func TestSecondUnauthorizedResponseIsTerminalWithoutARefreshLoop(t *testing.T) {
	provider := newRecordingConfluence(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusUnauthorized, `{"code":401,"message":"SENTINEL Unauthorized"}`)
	})
	credentials := &rejectionRefreshingCredentialProvider{}
	client, err := confluence.New(confluence.Config{CloudID: testCloudID, Endpoint: provider.URL}, credentials)
	require.NoError(t, err)

	result, err := sdkgo.RunQuery(newTestDexContext("no-refresh-loop"), client.GetPage(), confluenceConnection, confluence.GetPageInput{PageID: testPolicyPageID})
	require.NoError(t, err)
	require.Equal(t, confluence.GetPageBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
	require.Equal(t, "Confluence rejected the page with HTTP 401", result.Failure.Message)
	require.Equal(t, 2, provider.requestCount())
	require.Equal(t, 1, credentials.forcedRefreshes)
	requireNoSentinel(t, result)
}

func TestResponseThatReflectsTheAccessTokenIsNeverReturned(t *testing.T) {
	provider := newRecordingConfluence(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, pageJSON(t, testPolicyPageID, "token "+testAccessToken, 1, "", time.Now(), "<p>x</p>"))
	})
	client := newConfluenceClient(t, provider.URL)
	result, err := sdkgo.RunQuery(newTestDexContext("reflection"), client.GetPage(), confluenceConnection, confluence.GetPageInput{PageID: testPolicyPageID})
	require.NoError(t, err)
	require.Equal(t, confluence.GetPageBranchInvalidResponse, result.Branch)
	requireNoSentinel(t, result)
}

func TestRateLimitedReadRetriesAfterRetryAfter(t *testing.T) {
	provider := newRecordingConfluence(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		response.Header().Set("Retry-After", "7")
		writeJSON(t, response, http.StatusTooManyRequests, `{"message":"SENTINEL slow down"}`)
	})
	client := newConfluenceClient(t, provider.URL)
	_, err := sdkgo.RunQuery(newTestDexContext("rate-limit"), client.GetPage(), confluenceConnection, confluence.GetPageInput{PageID: testPolicyPageID})
	requireRetry(t, err, sdkgo.FailureRateLimit)
	var retryAfter *dex.RetryAfterError
	require.ErrorAs(t, err, &retryAfter)
	require.Equal(t, 7*time.Second, retryAfter.After)
}

func TestUnavailableCredentialsSelectDefectWithoutAProviderRequest(t *testing.T) {
	provider := newRecordingConfluence(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	client, err := confluence.New(confluence.Config{CloudID: testCloudID, Endpoint: provider.URL}, sdkgo.StaticCredentialProvider[confluence.Credentials]{})
	require.NoError(t, err)
	result, err := sdkgo.RunMutation(newTestDexContext("no-credentials"), client.CreatePage(), confluenceConnection, validCreatePageInput())
	require.NoError(t, err)
	require.Equal(t, confluence.CreatePageBranchDefect, result.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
}

func TestRedirectsAreNeverFollowed(t *testing.T) {
	provider := newRecordingConfluence(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		http.Redirect(response, request, "https://attacker.example/steal", http.StatusFound)
	})
	client := newConfluenceClient(t, provider.URL)
	result, err := sdkgo.RunQuery(newTestDexContext("redirect"), client.GetPage(), confluenceConnection, confluence.GetPageInput{PageID: testPolicyPageID})
	require.NoError(t, err)
	require.Equal(t, confluence.GetPageBranchInvalidResponse, result.Branch)
	require.Equal(t, 1, provider.requestCount())
}
