// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package github_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/github/go"
	"github.com/superdurable/dex-connectors-library/connectors/github/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestReplaceIssueLabelsSendsTheFullSetAndReturnsGitHubLabels(t *testing.T) {
	provider := newIssueLabelsProvider(t)
	client := newIssueClient(t, provider.server.URL, github.RepositoryAuthMethodID)

	result, err := sdkgo.RunMutation(testsupport.NewDexContext("issues-flow", "relabel"), client.ReplaceIssueLabels(), githubConnection, github.ReplaceIssueLabelsInput{
		Owner: " octocat ", Repository: "hello-world", IssueNumber: 7, Labels: []string{"bug", " needs triage "},
	})
	require.NoError(t, err)
	require.Equal(t, github.ReplaceIssueLabelsBranchReplaced, result.Branch, result.Failure)
	require.Equal(t, github.ReplacedIssueLabels{Number: 7, Labels: []string{"bug", "needs triage"}}, result.Value, "a loopback base URL has no known web host")
	require.Equal(t, sdkgo.IdempotencyKey(result.Receipt.CallID), result.Receipt.IdempotencyKey)

	cleared, err := sdkgo.RunMutation(testsupport.NewDexContext("issues-flow", "clear-labels"), client.ReplaceIssueLabels(), githubConnection, github.ReplaceIssueLabelsInput{
		Owner: "octocat", Repository: "hello-world", IssueNumber: 7,
	})
	require.NoError(t, err)
	require.Equal(t, github.ReplaceIssueLabelsBranchReplaced, cleared.Branch, cleared.Failure)
	require.Empty(t, cleared.Value.Labels)
	require.Equal(t, []string{`{"labels":["bug","needs triage"]}`, `{"labels":[]}`}, provider.sentBodies(), "an empty set is sent as an empty array")
	requireRedacted(t, result)
}

func TestReplaceIssueLabelsDerivesTheIssueWebURL(t *testing.T) {
	for baseURL, want := range map[string]string{
		"https://api.github.com":            "https://github.com/octocat/hello-world/issues/7",
		"https://github.example.com/api/v3": "https://github.example.com/octocat/hello-world/issues/7",
		"https://proxy.example.com/github":  "",
	} {
		t.Run(baseURL, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				writeJSON(t, response, []map[string]any{{"name": "bug"}})
			}))
			defer server.Close()
			client, err := github.New(github.Config{BaseURL: baseURL}, sdkgo.StaticCredentialProvider[github.Credentials]{githubConnection: {
				AuthMethodID: github.RepositoryAuthMethodID, AccessToken: sdkgo.NewSecretString("repository-token"),
			}}, github.WithHTTPClient(redirectingClient(server.URL)))
			require.NoError(t, err)
			result, err := sdkgo.RunMutation(testsupport.NewDexContext("issues-flow", "web-url"), client.ReplaceIssueLabels(), githubConnection, github.ReplaceIssueLabelsInput{
				Owner: "octocat", Repository: "hello-world", IssueNumber: 7, Labels: []string{"bug"},
			})
			require.NoError(t, err)
			require.Equal(t, want, result.Value.URL)
		})
	}
}

func TestReplaceIssueLabelsRetriesUnknownOutcomesBySendingTheSameSet(t *testing.T) {
	provider := newIssueLabelsProvider(t, http.StatusBadGateway, http.StatusTooManyRequests)
	client := newIssueClient(t, provider.server.URL, github.RepositoryAuthMethodID)
	ctx := testsupport.NewDexContext("issues-flow", "relabel-retry")
	input := github.ReplaceIssueLabelsInput{Owner: "octocat", Repository: "hello-world", IssueNumber: 7, Labels: []string{"bug"}}

	_, err := sdkgo.RunMutation(ctx, client.ReplaceIssueLabels(), githubConnection, input)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
	_, err = sdkgo.RunMutation(ctx, client.ReplaceIssueLabels(), githubConnection, input)
	var retryAfter *dex.RetryAfterError
	require.ErrorAs(t, err, &retryAfter)
	require.Equal(t, 3*time.Second, retryAfter.After)
	result, err := sdkgo.RunMutation(ctx, client.ReplaceIssueLabels(), githubConnection, input)
	require.NoError(t, err)
	require.Equal(t, github.ReplaceIssueLabelsBranchReplaced, result.Branch, result.Failure)
	require.Equal(t, []string{`{"labels":["bug"]}`, `{"labels":["bug"]}`, `{"labels":["bug"]}`}, provider.sentBodies())
}

func TestReplaceIssueLabelsClassifiesConclusiveAnswers(t *testing.T) {
	for _, test := range []struct {
		status int
		branch sdkgo.BranchID
		kind   sdkgo.FailureKind
	}{
		{status: http.StatusUnauthorized, branch: github.ReplaceIssueLabelsBranchAuthorizationRevoked, kind: sdkgo.FailureAuthentication},
		{status: http.StatusForbidden, branch: github.ReplaceIssueLabelsBranchInsufficientScope, kind: sdkgo.FailureAuthorization},
		{status: http.StatusNotFound, branch: github.ReplaceIssueLabelsBranchNotFound, kind: sdkgo.FailureNotFound},
		{status: http.StatusGone, branch: github.ReplaceIssueLabelsBranchProviderRejected, kind: sdkgo.FailureProviderRejection},
		{status: http.StatusUnprocessableEntity, branch: github.ReplaceIssueLabelsBranchProviderRejected, kind: sdkgo.FailureValidation},
	} {
		t.Run(http.StatusText(test.status), func(t *testing.T) {
			provider := newIssueLabelsProvider(t, test.status)
			client := newIssueClient(t, provider.server.URL, github.RepositoryAuthMethodID)
			result, err := sdkgo.RunMutation(testsupport.NewDexContext("issues-flow", "rejected"), client.ReplaceIssueLabels(), githubConnection, relabelInput())
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			require.Equal(t, test.kind, result.Failure.Kind)
			require.Equal(t, 7, result.Value.Number)
			requireRedacted(t, result)
		})
	}
}

func TestReplaceIssueLabelsRequiresTheRepositoryAuthMethodAndValidInput(t *testing.T) {
	provider := newIssueLabelsProvider(t)
	publicClient := newIssueClient(t, provider.server.URL, github.PublicAuthMethodID)
	result, err := sdkgo.RunMutation(testsupport.NewDexContext("issues-flow", "public"), publicClient.ReplaceIssueLabels(), githubConnection, relabelInput())
	require.NoError(t, err)
	require.Equal(t, github.ReplaceIssueLabelsBranchInsufficientScope, result.Branch)

	tooMany := make([]string, 101)
	for index := range tooMany {
		tooMany[index] = "label"
	}
	client := newIssueClient(t, provider.server.URL, github.RepositoryAuthMethodID)
	for name, input := range map[string]github.ReplaceIssueLabelsInput{
		"zero issue number": {Owner: "octocat", Repository: "hello-world", Labels: []string{"bug"}},
		"blank label":       {Owner: "octocat", Repository: "hello-world", IssueNumber: 7, Labels: []string{" "}},
		"long label":        {Owner: "octocat", Repository: "hello-world", IssueNumber: 7, Labels: []string{"a-label-name-that-runs-well-past-fifty-characters-long"}},
		"multiline label":   {Owner: "octocat", Repository: "hello-world", IssueNumber: 7, Labels: []string{"bug\nfix"}},
		"too many labels":   {Owner: "octocat", Repository: "hello-world", IssueNumber: 7, Labels: tooMany},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := sdkgo.RunMutation(testsupport.NewDexContext("issues-flow", "defect"), client.ReplaceIssueLabels(), githubConnection, input)
			require.NoError(t, err)
			require.Equal(t, github.ReplaceIssueLabelsBranchDefect, result.Branch)
			require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
		})
	}
	require.Empty(t, provider.sentBodies())
}

func relabelInput() github.ReplaceIssueLabelsInput {
	return github.ReplaceIssueLabelsInput{Owner: "octocat", Repository: "hello-world", IssueNumber: 7, Labels: []string{"bug"}}
}

// redirectingClient sends every request to target, so a client configured for a real host reaches a fake.
func redirectingClient(target string) *http.Client {
	return &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		redirected := request.Clone(request.Context())
		redirected.URL.Scheme, redirected.URL.Host = "http", target[len("http://"):]
		return http.DefaultTransport.RoundTrip(redirected)
	})}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (function roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

// issueLabelsProvider answers each PUT with the next scripted status, then applies the label set.
type issueLabelsProvider struct {
	server   *httptest.Server
	mutex    sync.Mutex
	statuses []int
	bodies   []string
}

func newIssueLabelsProvider(t *testing.T, statuses ...int) *issueLabelsProvider {
	t.Helper()
	provider := &issueLabelsProvider{statuses: statuses}
	provider.server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, "Bearer repository-token", request.Header.Get("Authorization"))
		require.Equal(t, http.MethodPut, request.Method)
		require.Equal(t, "/repos/octocat/hello-world/issues/7/labels", request.URL.Path)
		require.Equal(t, "application/json", request.Header.Get("Content-Type"))
		var body struct {
			Labels []string `json:"labels"`
		}
		raw := json.RawMessage{}
		require.NoError(t, json.NewDecoder(request.Body).Decode(&raw))
		require.NoError(t, json.Unmarshal(raw, &body))
		provider.mutex.Lock()
		provider.bodies = append(provider.bodies, string(raw))
		status := http.StatusOK
		if len(provider.statuses) > 0 {
			status, provider.statuses = provider.statuses[0], provider.statuses[1:]
		}
		provider.mutex.Unlock()
		if status == http.StatusTooManyRequests {
			response.Header().Set("Retry-After", "3")
		}
		if status != http.StatusOK {
			response.WriteHeader(status)
			_, _ = response.Write([]byte(`{"message":"` + providerBodySentinel + `"}`))
			return
		}
		labels := make([]map[string]any, 0, len(body.Labels))
		for index, name := range body.Labels {
			labels = append(labels, map[string]any{"id": index + 1, "name": name, "color": "d73a4a"})
		}
		writeJSON(t, response, labels)
	}))
	t.Cleanup(provider.server.Close)
	return provider
}

func (provider *issueLabelsProvider) sentBodies() []string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return append([]string(nil), provider.bodies...)
}
