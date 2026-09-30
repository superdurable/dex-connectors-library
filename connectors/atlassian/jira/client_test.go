// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jira_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/jira"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestNewRejectsInvalidSitesEndpointsAndLimits(t *testing.T) {
	credentials := staticJiraCredentials()
	_, err := jira.New(jira.Config{CloudID: "your-site.atlassian.net"}, credentials)
	require.ErrorContains(t, err, "cloudId must be a site UUID")
	_, err = jira.New(jira.Config{Endpoint: "http://api.atlassian.com"}, credentials)
	require.ErrorContains(t, err, "HTTPS")
	_, err = jira.New(jira.Config{MaxResponseBytes: -1}, credentials)
	require.Error(t, err)
	_, err = jira.New(jira.Config{}, nil)
	require.ErrorContains(t, err, "credential provider is required")
	_, err = jira.New(jira.Config{}, credentials, nil)
	require.ErrorContains(t, err, "option is nil")
	client, err := jira.New(jira.Config{CloudID: " " + testCloudID + " "}, credentials)
	require.NoError(t, err)
	require.NotNil(t, client)
}

func TestRequestsUseTheSiteGatewayPathAndTheBearerToken(t *testing.T) {
	provider := newRecordingJira(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, issueJSON("10042", "OPS-42", "Fire panel wiring", "3", "In Progress"))
	})
	client := newJiraClient(t, provider.URL)

	result, err := sdkgo.RunQuery(newJiraDexContext("site-path"), client.GetIssue(), jiraConnection, jira.GetIssueInput{IssueIDOrKey: "OPS-42"})
	require.NoError(t, err)
	require.Equal(t, jira.GetIssueBranchFound, result.Branch)
	request := provider.request(0)
	require.Equal(t, http.MethodGet, request.method)
	require.Equal(t, testSitePrefix+"/issue/OPS-42", request.path)
	require.Equal(t, "Bearer "+testAccessToken, request.authorization)
	require.Equal(t, "request-1", result.Receipt.ProviderRequestID)
	require.Equal(t, "OPS-42", result.Receipt.ProviderObjectID)
}

func TestBlankCloudIDUsesTheOnlyGrantedJiraSiteOnce(t *testing.T) {
	provider := newRecordingJira(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.URL.Path == "/oauth/token/accessible-resources" {
			writeJSON(t, response, http.StatusOK, `[`+
				`{"id":"99999999-0000-4000-8000-000000000000","name":"Wiki","url":"https://wiki.atlassian.net","scopes":["read:confluence-content.all"]},`+
				`{"id":"`+testCloudID+`","name":"Ops","url":"https://ops.atlassian.net","scopes":["read:jira-work","write:jira-work"]}]`)
			return
		}
		writeJSON(t, response, http.StatusOK, issueJSON("10042", "OPS-42", "Fire panel wiring", "3", "In Progress"))
	})
	client := newJiraClientForSite(t, provider.URL, "")

	for _, step := range []string{"first", "second"} {
		result, err := sdkgo.RunQuery(newJiraDexContext(step), client.GetIssue(), jiraConnection, jira.GetIssueInput{IssueIDOrKey: "OPS-42"})
		require.NoError(t, err)
		require.Equal(t, jira.GetIssueBranchFound, result.Branch)
	}
	require.Equal(t, 3, provider.requestCount(), "the site is looked up once and cached")
	require.Equal(t, "/oauth/token/accessible-resources", provider.request(0).path)
	require.Equal(t, testSitePrefix+"/issue/OPS-42", provider.request(1).path)
	require.Equal(t, testSitePrefix+"/issue/OPS-42", provider.request(2).path)
}

func TestBlankCloudIDSelectsDefectUnlessExactlyOneJiraSiteIsGranted(t *testing.T) {
	for _, test := range []struct {
		name      string
		resources string
		kind      sdkgo.FailureKind
		message   string
	}{
		{name: "no Jira site", resources: `[{"id":"` + testCloudID + `","scopes":["read:confluence-content.all"]}]`,
			kind: sdkgo.FailureAuthorization, message: "the authorization grants no Jira site; reconnect and pick a Jira site on the consent screen"},
		{name: "several Jira sites", resources: `[{"id":"` + testCloudID + `","scopes":["read:jira-work"]},{"id":"99999999-0000-4000-8000-000000000000","scopes":["write:jira-work"]}]`,
			kind: sdkgo.FailureValidation, message: "the authorization grants 2 Jira sites; choose one with the Jira site picker or set cloudId"},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingJira(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, http.StatusOK, test.resources)
			})
			client := newJiraClientForSite(t, provider.URL, "")
			result, err := sdkgo.RunQuery(newJiraDexContext("site-"+test.name), client.GetIssue(), jiraConnection, jira.GetIssueInput{IssueIDOrKey: "OPS-42"})
			require.NoError(t, err)
			require.Equal(t, jira.GetIssueBranchDefect, result.Branch)
			require.Equal(t, test.kind, result.Failure.Kind)
			require.Equal(t, test.message, result.Failure.Message)
			require.Equal(t, 1, provider.requestCount(), "no Jira request is sent without a site")
		})
	}
}

func TestAccessibleResourcesOutageRetriesTheStep(t *testing.T) {
	provider := newRecordingJira(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusServiceUnavailable, `{"message":"SENTINEL outage"}`)
	})
	client := newJiraClientForSite(t, provider.URL, "")
	_, err := sdkgo.RunMutation(newJiraDexContext("site-outage"), client.CreateIssue(), jiraConnection, validCreateIssueInput())
	requireRetry(t, err, sdkgo.FailureAvailability)
	require.Equal(t, 1, provider.requestCount(), "no create is sent while the site is unknown")
}

func TestUnauthorizedRequestRefreshesOnceAndResendsWithTheReplacement(t *testing.T) {
	provider := newRecordingJira(t, func(response http.ResponseWriter, request *http.Request, index int) {
		if index == 0 {
			require.Equal(t, "Bearer rejected-token", request.Header.Get("Authorization"))
			writeJSON(t, response, http.StatusUnauthorized, `{"code":401,"message":"SENTINEL Unauthorized"}`)
			return
		}
		require.Equal(t, "Bearer replacement-token", request.Header.Get("Authorization"))
		writeJSON(t, response, http.StatusCreated, `{"id":"10043","key":"OPS-43","self":"https://api.atlassian.com/issue/10043"}`)
	})
	credentials := &rejectionRefreshingCredentialProvider{}
	client, err := jira.New(jira.Config{CloudID: testCloudID, Endpoint: provider.URL}, credentials)
	require.NoError(t, err)

	result, err := sdkgo.RunMutation(newJiraDexContext("refresh-once"), client.CreateIssue(), jiraConnection, validCreateIssueInput())
	require.NoError(t, err)
	require.Equal(t, jira.CreateIssueBranchCreated, result.Branch, "a 401 proves Jira created nothing, so one resend is safe")
	require.Equal(t, 2, provider.requestCount())
	require.Equal(t, 1, credentials.forcedRefreshes)
}

func TestSecondUnauthorizedResponseIsTerminalWithoutARefreshLoop(t *testing.T) {
	provider := newRecordingJira(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusUnauthorized, `{"code":401,"message":"SENTINEL Unauthorized"}`)
	})
	credentials := &rejectionRefreshingCredentialProvider{}
	client, err := jira.New(jira.Config{CloudID: testCloudID, Endpoint: provider.URL}, credentials)
	require.NoError(t, err)

	result, err := sdkgo.RunQuery(newJiraDexContext("no-refresh-loop"), client.GetIssue(), jiraConnection, jira.GetIssueInput{IssueIDOrKey: "OPS-42"})
	require.NoError(t, err)
	require.Equal(t, jira.GetIssueBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
	require.Equal(t, 2, provider.requestCount())
	require.Equal(t, 1, credentials.forcedRefreshes)
	requireNoSentinel(t, result)
}

func TestFailuresNameStatusAndFieldIDsButNeverProviderMessages(t *testing.T) {
	provider := newRecordingJira(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusBadRequest, `{"errorMessages":["SENTINEL owner@example.com"],`+
			`"errors":{"summary":"SENTINEL summary too long","issuetype":"SENTINEL type","SENTINEL bad key!":"x"}}`)
	})
	client := newJiraClient(t, provider.URL)

	result, err := sdkgo.RunMutation(newJiraDexContext("safe-failure"), client.CreateIssue(), jiraConnection, validCreateIssueInput())
	require.NoError(t, err)
	require.Equal(t, jira.CreateIssueBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
	require.Equal(t, "Jira rejected the issue with HTTP 400 (fields: issuetype, summary)", result.Failure.Message)
	require.Equal(t, []string{"issuetype", "summary"}, result.Value.RejectedFieldIDs)
	requireNoSentinel(t, result)
}

func TestResponseThatReflectsTheAccessTokenIsNeverReturned(t *testing.T) {
	provider := newRecordingJira(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, issueJSON("10042", "OPS-42", "token "+testAccessToken, "3", "In Progress"))
	})
	client := newJiraClient(t, provider.URL)
	result, err := sdkgo.RunQuery(newJiraDexContext("reflection"), client.GetIssue(), jiraConnection, jira.GetIssueInput{IssueIDOrKey: "OPS-42"})
	require.NoError(t, err)
	require.Equal(t, jira.GetIssueBranchInvalidResponse, result.Branch)
	requireNoSentinel(t, result)
}

func TestRateLimitedReadRetriesAfterRetryAfter(t *testing.T) {
	provider := newRecordingJira(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		response.Header().Set("Retry-After", "7")
		writeJSON(t, response, http.StatusTooManyRequests, `{"errorMessages":["SENTINEL slow down"]}`)
	})
	client := newJiraClient(t, provider.URL)
	_, err := sdkgo.RunQuery(newJiraDexContext("rate-limit"), client.GetIssue(), jiraConnection, jira.GetIssueInput{IssueIDOrKey: "OPS-42"})
	requireRetry(t, err, sdkgo.FailureRateLimit)
	var retryAfter *dex.RetryAfterError
	require.ErrorAs(t, err, &retryAfter)
	require.Equal(t, 7*time.Second, retryAfter.After)
}

func TestUnavailableCredentialsSelectDefectWithoutAProviderRequest(t *testing.T) {
	provider := newRecordingJira(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	client, err := jira.New(jira.Config{CloudID: testCloudID, Endpoint: provider.URL}, sdkgo.StaticCredentialProvider[jira.Credentials]{})
	require.NoError(t, err)
	result, err := sdkgo.RunMutation(newJiraDexContext("no-credentials"), client.CreateIssue(), jiraConnection, validCreateIssueInput())
	require.NoError(t, err)
	require.Equal(t, jira.CreateIssueBranchDefect, result.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
}

func TestRedirectsAreNeverFollowed(t *testing.T) {
	provider := newRecordingJira(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		http.Redirect(response, request, "https://attacker.example/steal", http.StatusFound)
	})
	client := newJiraClient(t, provider.URL)
	result, err := sdkgo.RunQuery(newJiraDexContext("redirect"), client.GetIssue(), jiraConnection, jira.GetIssueInput{IssueIDOrKey: "OPS-42"})
	require.NoError(t, err)
	require.Equal(t, jira.GetIssueBranchInvalidResponse, result.Branch)
	require.Equal(t, 1, provider.requestCount())
}
