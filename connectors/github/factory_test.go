// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package githubconnector_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	githubconnector "github.com/superdurable/dex-connectors-library/connectors/github"
	connector "github.com/superdurable/dex-connectors-library/sdk/go"
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
	client, err := githubconnector.New(githubconnector.Config{}, connector.StaticCredentialProvider[githubconnector.Credentials]{
		githubConnection: {AccessToken: connector.NewSecretString("token")},
	})
	require.NoError(t, err)
	connection, err := githubconnector.NewConnection(client, githubConnection)
	require.NoError(t, err)

	profileResult := dex.DefineAttribute[connector.QueryResult[githubconnector.AuthenticatedProfile]]("github-profile-result")
	profile := githubconnector.NewGetAuthenticatedProfileStep(githubconnector.GetAuthenticatedProfileStepConfig[string]{
		StepType: "ReadGitHubProfile", Presentation: factoryPresentation(), Connection: connection,
		BuildInput: func(string) (githubconnector.GetAuthenticatedProfileInput, error) {
			return githubconnector.GetAuthenticatedProfileInput{}, nil
		},
		ProfileLoaded: connector.GoTo(profileTarget{}), VerifiedEmailRequired: connector.GoTo(profileTarget{}),
		InsufficientScope: connector.GoTo(profileTarget{}), AuthorizationRevoked: connector.GoTo(profileTarget{}),
		NotFound: connector.GoTo(profileTarget{}), Failed: connector.GoTo(profileTarget{}), Defect: connector.GoTo(profileTarget{}),
		ResultAttribute: &profileResult,
	})
	require.Equal(t, "ReadGitHubProfile", profile.GetStepType())

	repositoriesResult := dex.DefineAttribute[connector.QueryResult[githubconnector.PublicRepositories]]("github-repositories-result")
	repositories := githubconnector.NewListPublicRepositoriesStep(githubconnector.ListPublicRepositoriesStepConfig[string]{
		StepType: "ReadGitHubRepositories", Presentation: factoryPresentation(), Connection: connection,
		BuildInput: func(login string) (githubconnector.ListPublicRepositoriesInput, error) {
			return githubconnector.ListPublicRepositoriesInput{Login: login}, nil
		},
		RepositoriesLoaded: connector.GoTo(repositoriesTarget{}), InsufficientScope: connector.GoTo(repositoriesTarget{}),
		AuthorizationRevoked: connector.GoTo(repositoriesTarget{}), NotFound: connector.GoTo(repositoriesTarget{}),
		Failed: connector.GoTo(repositoriesTarget{}), Defect: connector.GoTo(repositoriesTarget{}),
		ResultAttribute: &repositoriesResult,
	})
	require.Equal(t, "ReadGitHubRepositories", repositories.GetStepType())
}

func TestTypedConnectionAndCredentialsCannotSerializeOrLeak(t *testing.T) {
	encoded, err := json.Marshal(githubconnector.Connection{})
	require.ErrorContains(t, err, "cannot be serialized")
	require.Nil(t, encoded)
	credentials := githubconnector.Credentials{AccessToken: connector.NewSecretString("secret-token")}
	encoded, err = json.Marshal(credentials)
	require.Error(t, err)
	require.Nil(t, encoded)
	require.NotContains(t, fmt.Sprintf("%#v", credentials), "secret-token")
}

func factoryPresentation() connector.StepPresentation {
	return connector.StepPresentation{GroupID: "github", GroupLabel: "GitHub", Explanation: "Read bounded signup profile evidence."}
}
