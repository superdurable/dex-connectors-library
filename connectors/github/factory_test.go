// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package githubconnector_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	githubconnector "github.com/superdurable/dex-connectors-library/connectors/github"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type profileTarget struct {
	dex.StepDefaultsNoWaitFor[githubconnector.GetAuthenticatedProfileStepOutput[string]]
}

func (profileTarget) Execute(dex.Context, githubconnector.GetAuthenticatedProfileStepOutput[string]) (*dex.StepDecision, error) {
	return dex.GracefulComplete(nil), nil
}

type repositoriesTarget struct {
	dex.StepDefaultsNoWaitFor[githubconnector.ListPublicRepositoriesStepOutput[string]]
}

func (repositoriesTarget) Execute(dex.Context, githubconnector.ListPublicRepositoriesStepOutput[string]) (*dex.StepDecision, error) {
	return dex.GracefulComplete(nil), nil
}

func TestGeneratedFactoriesExposeEveryTypedBranch(t *testing.T) {
	client, err := githubconnector.New(githubconnector.Config{}, sdkgo.StaticCredentialProvider[githubconnector.Credentials]{
		githubConnection: {AccessToken: sdkgo.NewSecretString("token")},
	})
	require.NoError(t, err)
	connection, err := githubconnector.NewConnection(client, githubConnection)
	require.NoError(t, err)

	profileResult := dex.DefineAttribute[sdkgo.QueryResult[githubconnector.AuthenticatedProfile]]("github-profile-result")
	profile := githubconnector.NewGetAuthenticatedProfileStep(githubconnector.GetAuthenticatedProfileStepConfig[string]{
		StepType: "ReadGitHubProfile", Presentation: factoryPresentation(), Connection: connection, ConnectionName: "signup",
		BuildInput: func(string) (githubconnector.GetAuthenticatedProfileInput, error) {
			return githubconnector.GetAuthenticatedProfileInput{}, nil
		},
		ProfileLoaded: sdkgo.GoTo(profileTarget{}), VerifiedEmailRequired: sdkgo.GoTo(profileTarget{}),
		InsufficientScope: sdkgo.GoTo(profileTarget{}), AuthorizationRevoked: sdkgo.GoTo(profileTarget{}),
		NotFound: sdkgo.GoTo(profileTarget{}), Failed: sdkgo.GoTo(profileTarget{}), Defect: sdkgo.GoTo(profileTarget{}),
		ResultAttribute: &profileResult,
	})
	require.Equal(t, "ReadGitHubProfile", profile.GetStepType())

	repositoriesResult := dex.DefineAttribute[sdkgo.QueryResult[githubconnector.PublicRepositories]]("github-repositories-result")
	repositories := githubconnector.NewListPublicRepositoriesStep(githubconnector.ListPublicRepositoriesStepConfig[string]{
		StepType: "ReadGitHubRepositories", Presentation: factoryPresentation(), Connection: connection, ConnectionName: "signup",
		BuildInput: func(login string) (githubconnector.ListPublicRepositoriesInput, error) {
			return githubconnector.ListPublicRepositoriesInput{Login: login}, nil
		},
		RepositoriesLoaded: sdkgo.GoTo(repositoriesTarget{}), InsufficientScope: sdkgo.GoTo(repositoriesTarget{}),
		AuthorizationRevoked: sdkgo.GoTo(repositoriesTarget{}), NotFound: sdkgo.GoTo(repositoriesTarget{}),
		Failed: sdkgo.GoTo(repositoriesTarget{}), Defect: sdkgo.GoTo(repositoriesTarget{}),
		ResultAttribute: &repositoriesResult,
	})
	require.Equal(t, "ReadGitHubRepositories", repositories.GetStepType())
}

func TestTypedConnectionAndCredentialsCannotSerializeOrLeak(t *testing.T) {
	encoded, err := json.Marshal(githubconnector.Connection{})
	require.ErrorContains(t, err, "cannot be serialized")
	require.Nil(t, encoded)
	credentials := githubconnector.Credentials{AccessToken: sdkgo.NewSecretString("secret-token")}
	encoded, err = json.Marshal(credentials)
	require.Error(t, err)
	require.Nil(t, encoded)
	require.NotContains(t, fmt.Sprintf("%#v", credentials), "secret-token")
}

func factoryPresentation() sdkgo.StepPresentation {
	return sdkgo.StepPresentation{GroupID: "github", GroupLabel: "GitHub", Explanation: "Read bounded signup profile evidence."}
}
