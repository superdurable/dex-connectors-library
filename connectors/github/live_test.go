//go:build live

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package github_test

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/github"
	"github.com/superdurable/dex-connectors-library/connectors/github/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestLiveAuthenticatedProfileAndPublicRepositories(t *testing.T) {
	token := os.Getenv("GITHUB_CONNECTOR_TEST_TOKEN")
	if token == "" {
		t.Skip("GITHUB_CONNECTOR_TEST_TOKEN is not configured")
	}
	credentials := sdkgo.StaticCredentialProvider[github.Credentials]{githubConnection: {
		AccessToken: sdkgo.NewSecretString(token),
	}}
	client, err := github.New(github.Config{}, credentials)
	require.NoError(t, err)
	profile, err := sdkgo.RunQuery(
		testsupport.NewDexContext("live-github-flow", "live-profile-step"), client.GetAuthenticatedProfile(), githubConnection,
		github.GetAuthenticatedProfileInput{},
	)
	require.NoError(t, err)
	require.Equal(t, github.GetAuthenticatedProfileBranchProfileLoaded, profile.Branch)
	require.NotEmpty(t, profile.Value.VerifiedEmail)
	repositories, err := sdkgo.RunQuery(
		testsupport.NewDexContext("live-github-flow", "live-repositories-step"), client.ListPublicRepositories(), githubConnection,
		github.ListPublicRepositoriesInput{Login: profile.Value.Login, Limit: 5},
	)
	require.NoError(t, err)
	require.Equal(t, github.ListPublicRepositoriesBranchRepositoriesLoaded, repositories.Branch)
	require.LessOrEqual(t, len(repositories.Value.Repositories), 5)
}

// TestLiveRepositoryChangeQueries reads up to five merged pull requests, files, and commits from 30 days,
// logging no token or body.
func TestLiveRepositoryChangeQueries(t *testing.T) {
	token := os.Getenv("GITHUB_CONNECTOR_TEST_TOKEN")
	if token == "" {
		t.Skip("GITHUB_CONNECTOR_TEST_TOKEN is not configured")
	}
	credentials := sdkgo.StaticCredentialProvider[github.Credentials]{githubConnection: {
		AccessToken: sdkgo.NewSecretString(token),
	}}
	client, err := github.New(github.Config{}, credentials)
	require.NoError(t, err)
	verifyLiveRepositoryChangeQueries(t, client, "superdurable", "dex")
}

func verifyLiveRepositoryChangeQueries(t *testing.T, client *github.Client, owner string, repository string) {
	t.Helper()
	windowEnd := time.Now().UTC().Truncate(time.Second)
	windowStart := windowEnd.AddDate(0, 0, -30)
	merged, err := sdkgo.RunQuery(
		testsupport.NewDexContext("live-github-flow", "live-merged-step"), client.ListMergedPullRequests(), githubConnection,
		github.ListMergedPullRequestsInput{Owner: owner, Repository: repository, MergedAfter: windowStart, MergedBefore: windowEnd, PageSize: 5},
	)
	require.NoError(t, err)
	require.Equal(t, github.ListMergedPullRequestsBranchListed, merged.Branch, "%+v", merged.Failure)
	require.LessOrEqual(t, len(merged.Value.PullRequests), 5)
	require.GreaterOrEqual(t, merged.Value.TotalCount, len(merged.Value.PullRequests))
	for _, pullRequest := range merged.Value.PullRequests {
		require.Positive(t, pullRequest.Number)
		require.False(t, pullRequest.MergedAt.Before(windowStart) || pullRequest.MergedAt.After(windowEnd), pullRequest.MergedAt)
		require.True(t, strings.HasPrefix(pullRequest.URL, "https://github.com/"+owner+"/"+repository+"/pull/"), pullRequest.URL)
	}
	if merged.Value.TotalCount > 5 {
		require.Equal(t, 2, merged.Value.NextPage)
	}
	if len(merged.Value.PullRequests) > 0 {
		files, err := sdkgo.RunQuery(
			testsupport.NewDexContext("live-github-flow", "live-files-step"), client.ListPullRequestFiles(), githubConnection,
			github.ListPullRequestFilesInput{Owner: owner, Repository: repository, Number: merged.Value.PullRequests[0].Number, PageSize: 5},
		)
		require.NoError(t, err)
		require.Equal(t, github.ListPullRequestFilesBranchListed, files.Branch, "%+v", files.Failure)
		require.LessOrEqual(t, len(files.Value.Files), 5)
		for _, file := range files.Value.Files {
			require.NotEmpty(t, file.Filename)
			require.NotEmpty(t, file.Status)
		}
	}
	commits, err := sdkgo.RunQuery(
		testsupport.NewDexContext("live-github-flow", "live-commits-step"), client.ListCommits(), githubConnection,
		github.ListCommitsInput{Owner: owner, Repository: repository, Since: windowStart, Until: windowEnd, PageSize: 5},
	)
	require.NoError(t, err)
	require.Equal(t, github.ListCommitsBranchListed, commits.Branch, "%+v", commits.Failure)
	require.LessOrEqual(t, len(commits.Value.Commits), 5)
	for _, commit := range commits.Value.Commits {
		require.Regexp(t, `^[0-9a-f]{40}$`, commit.SHA)
		require.False(t, commit.AuthoredAt.IsZero() || commit.CommittedAt.IsZero())
	}

	missingRepository := "dex-connector-live-test-missing-repository"
	rejected, err := sdkgo.RunQuery(
		testsupport.NewDexContext("live-github-flow", "live-missing-search-step"), client.ListMergedPullRequests(), githubConnection,
		github.ListMergedPullRequestsInput{Owner: owner, Repository: missingRepository, MergedAfter: windowStart, MergedBefore: windowEnd, PageSize: 1},
	)
	require.NoError(t, err)
	require.Equal(t, github.ListMergedPullRequestsBranchProviderRejected, rejected.Branch)
	missing, err := sdkgo.RunQuery(
		testsupport.NewDexContext("live-github-flow", "live-missing-commits-step"), client.ListCommits(), githubConnection,
		github.ListCommitsInput{Owner: owner, Repository: missingRepository, Since: windowStart, Until: windowEnd, PageSize: 1},
	)
	require.NoError(t, err)
	require.Equal(t, github.ListCommitsBranchNotFound, missing.Branch)
}
