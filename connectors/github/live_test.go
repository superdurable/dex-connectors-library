//go:build live

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package githubconnector_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	githubconnector "github.com/superdurable/dex-connectors-library/connectors/github"
	"github.com/superdurable/dex-connectors-library/connectors/github/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestLiveAuthenticatedProfileAndPublicRepositories(t *testing.T) {
	token := os.Getenv("GITHUB_CONNECTOR_TEST_TOKEN")
	if token == "" {
		t.Skip("GITHUB_CONNECTOR_TEST_TOKEN is not configured")
	}
	credentials := sdkgo.StaticCredentialProvider[githubconnector.Credentials]{githubConnection: {
		AccessToken: sdkgo.NewSecretString(token),
	}}
	client, err := githubconnector.New(githubconnector.Config{}, credentials)
	require.NoError(t, err)
	profile, err := sdkgo.RunQuery(
		testsupport.NewDexContext("live-github-flow", "live-profile-step"), client.GetAuthenticatedProfile(), githubConnection,
		githubconnector.GetAuthenticatedProfileInput{},
	)
	require.NoError(t, err)
	require.Equal(t, githubconnector.GetAuthenticatedProfileBranchProfileLoaded, profile.Branch)
	require.NotEmpty(t, profile.Value.VerifiedEmail)
	repositories, err := sdkgo.RunQuery(
		testsupport.NewDexContext("live-github-flow", "live-repositories-step"), client.ListPublicRepositories(), githubConnection,
		githubconnector.ListPublicRepositoriesInput{Login: profile.Value.Login, Limit: 5},
	)
	require.NoError(t, err)
	require.Equal(t, githubconnector.ListPublicRepositoriesBranchRepositoriesLoaded, repositories.Branch)
	require.LessOrEqual(t, len(repositories.Value.Repositories), 5)
}
