// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package githubconnector_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	githubconnector "github.com/superdurable/dex-connectors-library/connectors/github"
	"github.com/superdurable/dex-connectors-library/connectors/github/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var githubConnection = sdkgo.ConnectionRef{Provider: "github", Name: "signup"}

func TestGetAuthenticatedProfileReturnsBoundedProfileAndPrimaryVerifiedEmail(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		require.Equal(t, "Bearer one-use-token", request.Header.Get("Authorization"))
		require.Equal(t, "application/vnd.github+json", request.Header.Get("Accept"))
		require.Equal(t, "2026-03-10", request.Header.Get("X-GitHub-Api-Version"))
		response.Header().Set("X-OAuth-Scopes", "read:user, user:email")
		response.Header().Set("X-GitHub-Request-Id", "github-request-"+strconv.Itoa(int(requests.Load())))
		switch request.URL.Path {
		case "/user":
			writeJSON(t, response, map[string]any{
				"id": 42, "login": "octocat", "name": "Mona Lisa", "email": "raw@example.com",
				"avatar_url": "https://avatars.githubusercontent.com/u/42", "html_url": "https://github.com/octocat",
				"company": "GitHub", "location": "San Francisco", "bio": strings.Repeat("x", 1100),
				"public_repos": 12, "followers": 3, "following": 4,
			})
		case "/user/emails":
			require.Equal(t, "100", request.URL.Query().Get("per_page"))
			writeJSON(t, response, []map[string]any{
				{"email": "other@example.com", "primary": false, "verified": true},
				{"email": "Primary@Example.COM", "primary": true, "verified": true},
			})
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	client := newClient(t, server.URL, githubconnector.Config{})
	result, err := sdkgo.RunQuery(
		testsupport.NewDexContext("signup-flow", "profile-step"), client.GetAuthenticatedProfile(), githubConnection,
		githubconnector.GetAuthenticatedProfileInput{},
	)
	require.NoError(t, err)
	require.Equal(t, githubconnector.GetAuthenticatedProfileBranchProfileLoaded, result.Branch)
	require.Equal(t, "42", result.Value.Subject)
	require.Equal(t, "octocat", result.Value.Login)
	require.Equal(t, "primary@example.com", result.Value.VerifiedEmail)
	require.Len(t, result.Value.Bio, 1024)
	require.Equal(t, "github-request-2", result.Receipt.ProviderRequestID)
	require.Equal(t, "github-request-1", result.Receipt.Metadata["profileRequestId"])
	require.Equal(t, int32(2), requests.Load())

	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "one-use-token")
	require.NotContains(t, string(encoded), "raw@example.com")
}

func TestGetAuthenticatedProfileRequiresPrimaryVerifiedEmail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("X-OAuth-Scopes", "read:user, user:email")
		if request.URL.Path == "/user" {
			writeJSON(t, response, map[string]any{"id": 42, "login": "octocat"})
			return
		}
		writeJSON(t, response, []map[string]any{
			{"email": "primary@example.com", "primary": true, "verified": false},
			{"email": "secondary@example.com", "primary": false, "verified": true},
		})
	}))
	defer server.Close()

	client := newClient(t, server.URL, githubconnector.Config{})
	result, err := sdkgo.RunQuery(
		testsupport.NewDexContext("signup-flow", "email-step"), client.GetAuthenticatedProfile(), githubConnection,
		githubconnector.GetAuthenticatedProfileInput{},
	)
	require.NoError(t, err)
	require.Equal(t, githubconnector.GetAuthenticatedProfileBranchVerifiedEmailRequired, result.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
	require.Empty(t, result.Value.VerifiedEmail)
}

func TestGetAuthenticatedProfileClassifiesAuthorizationAndRateLimit(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		headers    map[string]string
		wantBranch sdkgo.BranchID
		wantKind   sdkgo.FailureKind
		wantRetry  time.Duration
	}{
		{name: "revoked", status: http.StatusUnauthorized, wantBranch: githubconnector.GetAuthenticatedProfileBranchAuthorizationRevoked, wantKind: sdkgo.FailureAuthentication},
		{name: "forbidden", status: http.StatusForbidden, wantBranch: githubconnector.GetAuthenticatedProfileBranchInsufficientScope, wantKind: sdkgo.FailureAuthorization},
		{name: "not found", status: http.StatusNotFound, wantBranch: githubconnector.GetAuthenticatedProfileBranchNotFound, wantKind: sdkgo.FailureNotFound},
		{name: "provider rejected", status: http.StatusBadRequest, wantBranch: githubconnector.GetAuthenticatedProfileBranchProviderRejected, wantKind: sdkgo.FailureProviderRejection},
		{name: "primary rate limit", status: http.StatusForbidden, headers: map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": "1767225635"}, wantKind: sdkgo.FailureRateLimit, wantRetry: 30 * time.Second},
		{name: "secondary rate limit", status: http.StatusTooManyRequests, headers: map[string]string{"Retry-After": "7"}, wantKind: sdkgo.FailureRateLimit, wantRetry: 7 * time.Second},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				for name, value := range test.headers {
					response.Header().Set(name, value)
				}
				response.WriteHeader(test.status)
			}))
			defer server.Close()
			client := newClient(t, server.URL, githubconnector.Config{}, githubconnector.WithClock(func() time.Time {
				return time.Unix(1767225605, 0)
			}))
			result, err := sdkgo.RunQuery(
				testsupport.NewDexContext("signup-flow", "classification-step"), client.GetAuthenticatedProfile(), githubConnection,
				githubconnector.GetAuthenticatedProfileInput{},
			)
			if test.wantRetry > 0 {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, test.wantKind, retry.Failure.Kind)
				var retryAfter *dex.RetryAfterError
				require.ErrorAs(t, err, &retryAfter)
				require.Equal(t, test.wantRetry, retryAfter.After)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.wantBranch, result.Branch)
			require.Equal(t, test.wantKind, result.Failure.Kind)
		})
	}
}

func TestGetAuthenticatedProfileRejectsMissingOAuthScopes(t *testing.T) {
	for _, scopes := range []string{"read:user", "read:user, user:email, repo"} {
		t.Run(scopes, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				response.Header().Set("X-OAuth-Scopes", scopes)
				writeJSON(t, response, map[string]any{"id": 42, "login": "octocat"})
			}))
			defer server.Close()

			client := newClient(t, server.URL, githubconnector.Config{})
			result, err := sdkgo.RunQuery(
				testsupport.NewDexContext("signup-flow", "scope-step"), client.GetAuthenticatedProfile(), githubConnection,
				githubconnector.GetAuthenticatedProfileInput{},
			)
			require.NoError(t, err)
			require.Equal(t, githubconnector.GetAuthenticatedProfileBranchInsufficientScope, result.Branch)
			require.Equal(t, int32(1), requests.Load())
		})
	}
}

func TestListPublicRepositoriesPaginatesDeduplicatesSortsAndTruncates(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		page := requests.Add(1)
		require.Equal(t, "/users/octocat/repos", request.URL.Path)
		require.Equal(t, "owner", request.URL.Query().Get("type"))
		require.Equal(t, "pushed", request.URL.Query().Get("sort"))
		require.Equal(t, "desc", request.URL.Query().Get("direction"))
		require.Equal(t, "100", request.URL.Query().Get("per_page"))
		response.Header().Set("X-OAuth-Scopes", "read:user, user:email")
		response.Header().Set("X-GitHub-Request-Id", "repo-request-"+strconv.Itoa(int(page)))
		if page == 1 {
			response.Header().Set("Link", `<https://api.github.com/users/octocat/repos?page=2>; rel="next"`)
			writeJSON(t, response, []map[string]any{
				repositoryJSON(4, "2026-04-04T00:00:00Z", false),
				repositoryJSON(99, "2026-04-03T12:00:00Z", true),
				repositoryJSON(2, "2026-04-03T00:00:00Z", false),
			})
			return
		}
		writeJSON(t, response, []map[string]any{
			repositoryJSON(2, "2026-04-03T00:00:00Z", false),
			repositoryJSON(3, "2026-04-02T00:00:00Z", false),
			repositoryJSON(1, "2026-04-01T00:00:00Z", false),
		})
	}))
	defer server.Close()

	client := newClient(t, server.URL, githubconnector.Config{DefaultRepositoryLimit: 3, MaxRepositories: 5})
	result, err := sdkgo.RunQuery(
		testsupport.NewDexContext("signup-flow", "repositories-step"), client.ListPublicRepositories(), githubConnection,
		githubconnector.ListPublicRepositoriesInput{Login: "octocat", Limit: 3},
	)
	require.NoError(t, err)
	require.Equal(t, githubconnector.ListPublicRepositoriesBranchRepositoriesLoaded, result.Branch)
	require.True(t, result.Value.Truncated)
	require.Equal(t, []int64{4, 2, 3}, []int64{
		result.Value.Repositories[0].ID, result.Value.Repositories[1].ID, result.Value.Repositories[2].ID,
	})
	require.Equal(t, "2", result.Receipt.Metadata["pagesRead"])
	require.Equal(t, int32(2), requests.Load())
	for _, repository := range result.Value.Repositories {
		require.NotEqual(t, int64(99), repository.ID)
	}
}

func TestListPublicRepositoriesValidatesInputBeforeProviderAccess(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	defer server.Close()
	client := newClient(t, server.URL, githubconnector.Config{DefaultRepositoryLimit: 5, MaxRepositories: 5})

	for _, input := range []githubconnector.ListPublicRepositoriesInput{{Login: ""}, {Login: "bad/login"}, {Login: "octocat", Limit: 6}} {
		result, err := sdkgo.RunQuery(
			testsupport.NewDexContext("signup-flow", "invalid-repositories-step"), client.ListPublicRepositories(), githubConnection, input,
		)
		require.NoError(t, err)
		require.Equal(t, githubconnector.ListPublicRepositoriesBranchDefect, result.Branch)
		require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
	}
	require.Zero(t, requests.Load())
}

func TestResponseSizeFailureIsTerminalAndSecretSafe(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("X-OAuth-Scopes", "read:user, user:email")
		_, _ = response.Write([]byte(strings.Repeat("x", 100)))
	}))
	defer server.Close()
	client := newClient(t, server.URL, githubconnector.Config{MaxResponseBytes: 16})
	result, err := sdkgo.RunQuery(
		testsupport.NewDexContext("signup-flow", "large-response-step"), client.GetAuthenticatedProfile(), githubConnection,
		githubconnector.GetAuthenticatedProfileInput{},
	)
	require.NoError(t, err)
	require.Equal(t, githubconnector.GetAuthenticatedProfileBranchInvalidResponse, result.Branch)
	require.Equal(t, sdkgo.FailureResponseTooLarge, result.Failure.Kind)
	require.NotContains(t, fmt.Sprintf("%#v", result), "one-use-token")
}

func TestTransportAndAvailabilityFailuresAreSafeRetries(t *testing.T) {
	transportClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		require.Equal(t, "Bearer one-use-token", request.Header.Get("Authorization"))
		return nil, errors.New("transport failed with one-use-token")
	})}
	client := newClient(t, "https://api.github.test", githubconnector.Config{}, githubconnector.WithHTTPClient(transportClient))
	_, err := sdkgo.RunQuery(
		testsupport.NewDexContext("signup-flow", "transport-step"), client.GetAuthenticatedProfile(), githubConnection,
		githubconnector.GetAuthenticatedProfileInput{},
	)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
	require.NotContains(t, err.Error(), "one-use-token")

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	client = newClient(t, server.URL, githubconnector.Config{})
	_, err = sdkgo.RunQuery(
		testsupport.NewDexContext("signup-flow", "availability-step"), client.GetAuthenticatedProfile(), githubConnection,
		githubconnector.GetAuthenticatedProfileInput{},
	)
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
}

func TestConfigRejectsUnsafeEndpointAndRepositoryLimits(t *testing.T) {
	credentials := sdkgo.StaticCredentialProvider[githubconnector.Credentials]{githubConnection: {
		AccessToken: sdkgo.NewSecretString("one-use-token"),
	}}
	_, err := githubconnector.New(githubconnector.Config{BaseURL: "http://github.example"}, credentials)
	require.ErrorContains(t, err, "must use HTTPS")
	_, err = githubconnector.New(githubconnector.Config{BaseURL: "https://token@api.github.com"}, credentials)
	require.ErrorContains(t, err, "cannot contain user info")
	_, err = githubconnector.New(githubconnector.Config{MaxRepositories: 501}, credentials)
	require.ErrorContains(t, err, "cannot exceed 500")
	_, err = githubconnector.New(githubconnector.Config{APIVersion: "latest"}, credentials)
	require.ErrorContains(t, err, "YYYY-MM-DD")
}

func newClient(t *testing.T, baseURL string, config githubconnector.Config, options ...githubconnector.Option) *githubconnector.Client {
	t.Helper()
	config.BaseURL = baseURL
	credentials := sdkgo.StaticCredentialProvider[githubconnector.Credentials]{githubConnection: {
		AccessToken: sdkgo.NewSecretString("one-use-token"),
	}}
	client, err := githubconnector.New(config, credentials, options...)
	require.NoError(t, err)
	return client
}

func writeJSON(t *testing.T, response http.ResponseWriter, value any) {
	t.Helper()
	response.Header().Set("Content-Type", "application/json")
	require.NoError(t, json.NewEncoder(response).Encode(value))
}

func repositoryJSON(id int64, pushedAt string, private bool) map[string]any {
	visibility := "public"
	if private {
		visibility = "private"
	}
	return map[string]any{
		"id": id, "name": fmt.Sprintf("repo-%d", id), "full_name": fmt.Sprintf("octocat/repo-%d", id),
		"private": private, "visibility": visibility, "html_url": fmt.Sprintf("https://github.com/octocat/repo-%d", id),
		"description": "Public repository", "language": "Go", "topics": []string{"dex", "ai"},
		"pushed_at": pushedAt, "created_at": "2026-01-01T00:00:00Z", "updated_at": pushedAt,
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
