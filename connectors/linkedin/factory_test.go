// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package linkedinconnector_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	linkedinconnector "github.com/superdurable/dex-connectors-library/connectors/linkedin"
	connector "github.com/superdurable/dex-connectors-library/sdk/go"
	"github.com/superdurable/dex/sdk-go/dex"
)

type profileTarget struct {
	dex.StepDefaultsNoWaitFor[linkedinconnector.GetAuthenticatedProfileStepOutput[string]]
}

func (profileTarget) Execute(dex.Context, linkedinconnector.GetAuthenticatedProfileStepOutput[string]) (*dex.StepDecision, error) {
	return dex.GracefulComplete(nil), nil
}

func TestGeneratedFactoryExposesEveryTypedBranch(t *testing.T) {
	client, err := linkedinconnector.New(linkedinconnector.Config{}, connector.StaticCredentialProvider[linkedinconnector.Credentials]{
		linkedinConnection: {AccessToken: connector.NewSecretString("token")},
	})
	require.NoError(t, err)
	connection, err := linkedinconnector.NewConnection(client, linkedinConnection)
	require.NoError(t, err)
	result := dex.DefineAttribute[connector.QueryResult[linkedinconnector.AuthenticatedProfile]]("linkedin-profile-result")
	step := linkedinconnector.NewGetAuthenticatedProfileStep(linkedinconnector.GetAuthenticatedProfileStepConfig[string]{
		StepType:     "ReadLinkedInProfile",
		Presentation: connector.StepPresentation{GroupID: "linkedin", GroupLabel: "LinkedIn", Explanation: "Read bounded signup identity claims."},
		Connection:   connection,
		BuildInput: func(string) (linkedinconnector.GetAuthenticatedProfileInput, error) {
			return linkedinconnector.GetAuthenticatedProfileInput{}, nil
		},
		ProfileLoaded: connector.GoTo(profileTarget{}), VerifiedEmailRequired: connector.GoTo(profileTarget{}),
		InsufficientScope: connector.GoTo(profileTarget{}), AuthorizationRevoked: connector.GoTo(profileTarget{}),
		NotFound: connector.GoTo(profileTarget{}), Failed: connector.GoTo(profileTarget{}), Defect: connector.GoTo(profileTarget{}),
		ResultAttribute: &result,
	})
	require.Equal(t, "ReadLinkedInProfile", step.GetStepType())
}

func TestTypedConnectionAndCredentialsCannotSerializeOrLeak(t *testing.T) {
	encoded, err := json.Marshal(linkedinconnector.Connection{})
	require.ErrorContains(t, err, "cannot be serialized")
	require.Nil(t, encoded)
	credentials := linkedinconnector.Credentials{AccessToken: connector.NewSecretString("secret-token")}
	encoded, err = json.Marshal(credentials)
	require.Error(t, err)
	require.Nil(t, encoded)
	require.NotContains(t, fmt.Sprintf("%#v", credentials), "secret-token")
}
