// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jiraservicemanagement_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	jiraservicemanagement "github.com/superdurable/dex-connectors-library/connectors/atlassian/jira-service-management"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var searchInput = jiraservicemanagement.SearchTicketsInput{ProjectKey: "ITH"}

const emptySearchPage = `{"issues":[]}`

func TestNewRejectsInvalidSitesEndpointsAndLimits(t *testing.T) {
	credentials := staticTestCredentials()
	_, err := jiraservicemanagement.New(jiraservicemanagement.Config{CloudID: "your-site.atlassian.net"}, credentials)
	require.ErrorContains(t, err, "cloudId must be a site UUID")
	_, err = jiraservicemanagement.New(jiraservicemanagement.Config{Endpoint: "http://api.atlassian.com"}, credentials)
	require.ErrorContains(t, err, "HTTPS")
	_, err = jiraservicemanagement.New(jiraservicemanagement.Config{MaxResponseBytes: -1}, credentials)
	require.Error(t, err)
	_, err = jiraservicemanagement.New(jiraservicemanagement.Config{}, nil)
	require.ErrorContains(t, err, "credential provider is required")
	_, err = jiraservicemanagement.New(jiraservicemanagement.Config{}, credentials, nil)
	require.ErrorContains(t, err, "option is nil")
	client, err := jiraservicemanagement.New(jiraservicemanagement.Config{CloudID: " " + testCloudID + " "}, credentials)
	require.NoError(t, err)
	require.NotNil(t, client)
}

func TestBlankCloudIDUsesTheOnlyGrantedServiceDeskSiteOnce(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.URL.Path == "/oauth/token/accessible-resources" {
			writeJSON(t, response, http.StatusOK, `[`+
				`{"id":"99999999-0000-4000-8000-000000000000","name":"Wiki","scopes":["read:confluence-content.all"]},`+
				`{"id":"`+testCloudID+`","name":"Help","url":"https://help.atlassian.net","scopes":["read:servicedesk-request","read:jira-work"]}]`)
			return
		}
		writeJSON(t, response, http.StatusOK, emptySearchPage)
	})
	client := newTestClientForSite(t, provider.URL, "")
	for _, step := range []string{"first", "second"} {
		result, err := sdkgo.RunQuery(newTestDexContext(step), client.SearchTickets(), jsmConnection, searchInput)
		require.NoError(t, err)
		require.Equal(t, jiraservicemanagement.SearchTicketsBranchSearched, result.Branch)
	}
	require.Equal(t, 3, provider.requestCount(), "the site is looked up once and cached")
	require.Equal(t, testPlatformPrefix+"/search/jql", provider.request(2).path)
	require.Equal(t, "Bearer "+testAccessToken, provider.request(2).authorization)
}

func TestBlankCloudIDSelectsDefectUnlessExactlyOneSiteIsGranted(t *testing.T) {
	for _, test := range []struct {
		name      string
		resources string
		kind      sdkgo.FailureKind
	}{
		{"Jira only", `[{"id":"` + testCloudID + `","scopes":["read:jira-work"]}]`, sdkgo.FailureAuthorization},
		{"several", `[{"id":"` + testCloudID + `","scopes":["read:servicedesk-request"]},{"id":"99999999-0000-4000-8000-000000000000","scopes":["write:servicedesk-request"]}]`, sdkgo.FailureValidation},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, http.StatusOK, test.resources)
			})
			result, err := sdkgo.RunQuery(newTestDexContext("site-"+test.name), newTestClientForSite(t, provider.URL, "").SearchTickets(), jsmConnection, searchInput)
			require.NoError(t, err)
			require.Equal(t, jiraservicemanagement.SearchTicketsBranchDefect, result.Branch)
			require.Equal(t, test.kind, result.Failure.Kind)
			require.Equal(t, 1, provider.requestCount(), "nothing is sent without a site")
		})
	}
}

func TestUnauthorizedRequestRefreshesOnceAndResendsWithTheReplacement(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, request *http.Request, index int) {
		if index == 0 {
			require.Equal(t, "Bearer rejected-token", request.Header.Get("Authorization"))
			writeJSON(t, response, http.StatusUnauthorized, `{"code":401,"message":"SENTINEL Unauthorized"}`)
			return
		}
		require.Equal(t, "Bearer replacement-token", request.Header.Get("Authorization"))
		writeJSON(t, response, http.StatusCreated, createdRequestJSON)
	})
	credentials := &rejectionRefreshingCredentialProvider{}
	client, err := jiraservicemanagement.New(jiraservicemanagement.Config{CloudID: testCloudID, Endpoint: provider.URL}, credentials)
	require.NoError(t, err)
	result, err := sdkgo.RunMutation(newTestDexContext("refresh-once"), client.CreateTicket(), jsmConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, jiraservicemanagement.CreateTicketBranchCreated, result.Branch, "a 401 proves nothing was created, so one resend is safe")
	require.Equal(t, 2, provider.requestCount())
	require.Equal(t, 1, credentials.forcedRefreshes)
}

func TestSecondUnauthorizedResponseIsTerminalWithoutARefreshLoop(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusUnauthorized, `{"code":401,"message":"SENTINEL Unauthorized"}`)
	})
	credentials := &rejectionRefreshingCredentialProvider{}
	client, err := jiraservicemanagement.New(jiraservicemanagement.Config{CloudID: testCloudID, Endpoint: provider.URL}, credentials)
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(newTestDexContext("no-refresh-loop"), client.SearchTickets(), jsmConnection, searchInput)
	require.NoError(t, err)
	require.Equal(t, jiraservicemanagement.SearchTicketsBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
	require.Equal(t, 2, provider.requestCount())
	require.Equal(t, 1, credentials.forcedRefreshes)
	requireNoSentinel(t, result)
}

func TestResponsesThatAreOversizedRedirectedOrReflectTheTokenAreNeverReturned(t *testing.T) {
	for name, reply := range map[string]func(http.ResponseWriter, *http.Request){
		"reflection": func(response http.ResponseWriter, _ *http.Request) {
			writeJSON(t, response, http.StatusOK, `{"issues":[],"note":"`+testAccessToken+`"}`)
		},
		"oversized": func(response http.ResponseWriter, _ *http.Request) {
			writeJSON(t, response, http.StatusOK, `{"issues":[],"padding":"`+strings.Repeat("x", 2048)+`"}`)
		},
		"redirect": func(response http.ResponseWriter, request *http.Request) {
			http.Redirect(response, request, "https://attacker.example/steal", http.StatusFound)
		},
	} {
		provider := newRecordingProvider(t, func(response http.ResponseWriter, request *http.Request, _ int) { reply(response, request) })
		client, err := jiraservicemanagement.New(jiraservicemanagement.Config{CloudID: testCloudID, Endpoint: provider.URL, MaxResponseBytes: 1024}, staticTestCredentials())
		require.NoError(t, err)
		result, err := sdkgo.RunQuery(newTestDexContext(name), client.SearchTickets(), jsmConnection, searchInput)
		require.NoError(t, err, name)
		require.Equal(t, jiraservicemanagement.SearchTicketsBranchInvalidResponse, result.Branch, name)
		require.Equal(t, 1, provider.requestCount(), name)
		requireNoSentinel(t, result)
	}
}

func TestRateLimitedReadRetriesAfterRetryAfter(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		response.Header().Set("Retry-After", "7")
		writeJSON(t, response, http.StatusTooManyRequests, `{"errorMessages":["SENTINEL slow down"]}`)
	})
	_, err := sdkgo.RunQuery(newTestDexContext("rate-limit"), newTestClient(t, provider.URL).SearchTickets(), jsmConnection, searchInput)
	requireRetry(t, err, sdkgo.FailureRateLimit)
	var retryAfter *dex.RetryAfterError
	require.ErrorAs(t, err, &retryAfter)
	require.Equal(t, 7*time.Second, retryAfter.After)
}

func TestUnavailableCredentialsSelectDefectWithoutAProviderRequest(t *testing.T) {
	provider := newRecordingProvider(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	client, err := jiraservicemanagement.New(jiraservicemanagement.Config{CloudID: testCloudID, Endpoint: provider.URL},
		sdkgo.StaticCredentialProvider[jiraservicemanagement.Credentials]{})
	require.NoError(t, err)
	result, err := sdkgo.RunMutation(newTestDexContext("no-credentials"), client.CreateTicket(), jsmConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, jiraservicemanagement.CreateTicketBranchDefect, result.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
}
