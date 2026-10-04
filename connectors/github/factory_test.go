// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package github_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/github"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type profileTarget struct {
	dex.StepDefaultsNoWaitFor[github.GetAuthenticatedProfileResult]
}

func (profileTarget) Execute(dex.Context, github.GetAuthenticatedProfileResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(nil), nil
}

type repositoriesTarget struct {
	dex.StepDefaultsNoWaitFor[github.ListPublicRepositoriesResult]
}

func (repositoriesTarget) Execute(dex.Context, github.ListPublicRepositoriesResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(nil), nil
}

type mergedPullRequestsTarget struct {
	dex.StepDefaultsNoWaitFor[github.ListMergedPullRequestsResult]
}

func (mergedPullRequestsTarget) Execute(dex.Context, github.ListMergedPullRequestsResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(nil), nil
}

type pullRequestFilesTarget struct {
	dex.StepDefaultsNoWaitFor[github.ListPullRequestFilesResult]
}

func (pullRequestFilesTarget) Execute(dex.Context, github.ListPullRequestFilesResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(nil), nil
}

type commitsTarget struct {
	dex.StepDefaultsNoWaitFor[github.ListCommitsResult]
}

func (commitsTarget) Execute(dex.Context, github.ListCommitsResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(nil), nil
}

func TestGeneratedRepositoryChangeFactoriesExposeEveryTypedBranch(t *testing.T) {
	client, err := github.New(github.Config{}, sdkgo.StaticCredentialProvider[github.Credentials]{
		githubConnection: {AccessToken: sdkgo.NewSecretString("token")},
	})
	require.NoError(t, err)
	connection, err := github.NewConnection(client, githubConnection)
	require.NoError(t, err)

	mergedResult := dex.DefineAttribute[github.ListMergedPullRequestsResult]("github-merged-pull-requests-result")
	merged := github.NewListMergedPullRequestsStep(github.ListMergedPullRequestsStepConfig[string]{
		StepType: "ListMergedPullRequests", Annotations: factoryAnnotations(), Connection: connection, ConnectionName: "signup",
		MapToOperationInput: func(repository string) github.ListMergedPullRequestsInput {
			return github.ListMergedPullRequestsInput{Owner: "octocat", Repository: repository}
		},
		Listed: sdkgo.GoTo(mergedPullRequestsTarget{}), InsufficientScope: sdkgo.GoTo(mergedPullRequestsTarget{}),
		AuthorizationRevoked: sdkgo.GoTo(mergedPullRequestsTarget{}), NotFound: sdkgo.GoTo(mergedPullRequestsTarget{}),
		ProviderRejected: sdkgo.GoTo(mergedPullRequestsTarget{}), InvalidResponse: sdkgo.GoTo(mergedPullRequestsTarget{}),
		Defect: sdkgo.GoTo(mergedPullRequestsTarget{}), ResultAttribute: &mergedResult,
	})
	require.Equal(t, "ListMergedPullRequests", merged.GetStepType())
	requireAsyncQueryDefaults(t, merged.GetStepOptions())

	filesResult := dex.DefineAttribute[github.ListPullRequestFilesResult]("github-pull-request-files-result")
	files := github.NewListPullRequestFilesStep(github.ListPullRequestFilesStepConfig[int]{
		StepType: "ListPullRequestFiles", Annotations: factoryAnnotations(), Connection: connection, ConnectionName: "signup",
		MapToOperationInput: func(number int) github.ListPullRequestFilesInput {
			return github.ListPullRequestFilesInput{Owner: "octocat", Repository: "hello-world", Number: number}
		},
		Listed: sdkgo.GoTo(pullRequestFilesTarget{}), InsufficientScope: sdkgo.GoTo(pullRequestFilesTarget{}),
		AuthorizationRevoked: sdkgo.GoTo(pullRequestFilesTarget{}), NotFound: sdkgo.GoTo(pullRequestFilesTarget{}),
		ProviderRejected: sdkgo.GoTo(pullRequestFilesTarget{}), InvalidResponse: sdkgo.GoTo(pullRequestFilesTarget{}),
		Defect: sdkgo.GoTo(pullRequestFilesTarget{}), ResultAttribute: &filesResult,
	})
	require.Equal(t, "ListPullRequestFiles", files.GetStepType())
	requireAsyncQueryDefaults(t, files.GetStepOptions())

	commitsResult := dex.DefineAttribute[github.ListCommitsResult]("github-commits-result")
	commits := github.NewListCommitsStep(github.ListCommitsStepConfig[string]{
		StepType: "ListCommits", Annotations: factoryAnnotations(), Connection: connection, ConnectionName: "signup",
		MapToOperationInput: func(repository string) github.ListCommitsInput {
			return github.ListCommitsInput{Owner: "octocat", Repository: repository}
		},
		Listed: sdkgo.GoTo(commitsTarget{}), InsufficientScope: sdkgo.GoTo(commitsTarget{}),
		AuthorizationRevoked: sdkgo.GoTo(commitsTarget{}), NotFound: sdkgo.GoTo(commitsTarget{}),
		ProviderRejected: sdkgo.GoTo(commitsTarget{}), InvalidResponse: sdkgo.GoTo(commitsTarget{}),
		Defect: sdkgo.GoTo(commitsTarget{}), ResultAttribute: &commitsResult,
	})
	require.Equal(t, "ListCommits", commits.GetStepType())
	requireAsyncQueryDefaults(t, commits.GetStepOptions())

	require.Panics(t, func() {
		github.NewListCommitsStep(github.ListCommitsStepConfig[string]{
			StepType: "ListCommitsWithoutHappyPath", Annotations: factoryAnnotations(), Connection: connection,
			ConnectionName:      githubConnection.Name,
			MapToOperationInput: func(string) github.ListCommitsInput { return github.ListCommitsInput{} },
		})
	}, "the listed branch is required")
}

// requireAsyncQueryDefaults checks the 65-minute retry budget; Dex fails a Step whose RetryAfter delay
// exceeds the remaining budget.
func requireAsyncQueryDefaults(t *testing.T, options *dex.StepOptions) {
	t.Helper()
	require.Equal(t, dex.StepDurabilityAsync, options.ExecuteDurability)
	require.Equal(t, 30*time.Second, options.ExecuteMethodTimeout)
	require.Equal(t, int32(5), options.ExecuteRetry.MaximumAttempts)
	require.Equal(t, 65*time.Minute, options.ExecuteRetry.TotalDuration)
	require.Greater(t, options.ExecuteRetry.TotalDuration, time.Hour, "a rate-limited query can wait for GitHub's hourly reset")
}

func TestGeneratedFactoriesExposeEveryTypedBranch(t *testing.T) {
	client, err := github.New(github.Config{}, sdkgo.StaticCredentialProvider[github.Credentials]{
		githubConnection: {AccessToken: sdkgo.NewSecretString("token")},
	})
	require.NoError(t, err)
	connection, err := github.NewConnection(client, githubConnection)
	require.NoError(t, err)

	profileResult := dex.DefineAttribute[sdkgo.QueryResult[github.AuthenticatedProfile]]("github-profile-result")
	profile := github.NewGetAuthenticatedProfileStep(github.GetAuthenticatedProfileStepConfig[string]{
		StepType: "ReadGitHubProfile", Annotations: factoryAnnotations(), Connection: connection, ConnectionName: "signup",
		MapToOperationInput: func(string) github.GetAuthenticatedProfileInput {
			return github.GetAuthenticatedProfileInput{}
		},
		ProfileLoaded: sdkgo.GoTo(profileTarget{}), VerifiedEmailRequired: sdkgo.GoTo(profileTarget{}),
		InsufficientScope: sdkgo.GoTo(profileTarget{}), AuthorizationRevoked: sdkgo.GoTo(profileTarget{}),
		NotFound: sdkgo.GoTo(profileTarget{}), ProviderRejected: sdkgo.GoTo(profileTarget{}),
		InvalidResponse: sdkgo.GoTo(profileTarget{}), Defect: sdkgo.GoTo(profileTarget{}),
		ResultAttribute: &profileResult,
	})
	require.Equal(t, "ReadGitHubProfile", profile.GetStepType())

	repositoriesResult := dex.DefineAttribute[sdkgo.QueryResult[github.PublicRepositories]]("github-repositories-result")
	repositories := github.NewListPublicRepositoriesStep(github.ListPublicRepositoriesStepConfig[string]{
		StepType: "ReadGitHubRepositories", Annotations: factoryAnnotations(), Connection: connection, ConnectionName: "signup",
		MapToOperationInput: func(login string) github.ListPublicRepositoriesInput {
			return github.ListPublicRepositoriesInput{Login: login}
		},
		RepositoriesLoaded: sdkgo.GoTo(repositoriesTarget{}), InsufficientScope: sdkgo.GoTo(repositoriesTarget{}),
		AuthorizationRevoked: sdkgo.GoTo(repositoriesTarget{}), NotFound: sdkgo.GoTo(repositoriesTarget{}),
		ProviderRejected: sdkgo.GoTo(repositoriesTarget{}), InvalidResponse: sdkgo.GoTo(repositoriesTarget{}),
		Defect:          sdkgo.GoTo(repositoriesTarget{}),
		ResultAttribute: &repositoriesResult,
	})
	require.Equal(t, "ReadGitHubRepositories", repositories.GetStepType())
	for _, options := range []*dex.StepOptions{profile.GetStepOptions(), repositories.GetStepOptions()} {
		require.Equal(t, 2*time.Minute, options.ExecuteRetry.TotalDuration, "signup queries keep the two-minute retry budget")
	}
}

func TestTypedConnectionAndCredentialsCannotSerializeOrLeak(t *testing.T) {
	encoded, err := json.Marshal(github.Connection{})
	require.ErrorContains(t, err, "cannot be serialized")
	require.Nil(t, encoded)
	credentials := github.Credentials{AccessToken: sdkgo.NewSecretString("secret-token")}
	encoded, err = json.Marshal(credentials)
	require.Error(t, err)
	require.Nil(t, encoded)
	require.NotContains(t, fmt.Sprintf("%#v", credentials), "secret-token")
}

func factoryAnnotations() sdkgo.StepAnnotations {
	return sdkgo.StepAnnotations{GroupID: "github", GroupLabel: "GitHub", Explanation: "Read bounded signup profile evidence."}
}
