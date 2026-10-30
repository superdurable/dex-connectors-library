// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package linkedin_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/linkedin"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type profileTarget struct {
	dex.StepDefaultsNoWaitFor[linkedin.GetAuthenticatedProfileResult]
}

func (profileTarget) Execute(dex.Context, linkedin.GetAuthenticatedProfileResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(nil), nil
}

func TestGeneratedFactoryExposesEveryTypedBranch(t *testing.T) {
	client, err := linkedin.New(linkedin.Config{}, sdkgo.StaticCredentialProvider[linkedin.Credentials]{
		linkedinConnection: {AccessToken: sdkgo.NewSecretString("token")},
	})
	require.NoError(t, err)
	connection, err := linkedin.NewConnection(client, linkedinConnection)
	require.NoError(t, err)
	result := dex.DefineAttribute[sdkgo.QueryResult[linkedin.AuthenticatedProfile]]("linkedin-profile-result")
	step := linkedin.NewGetAuthenticatedProfileStep(linkedin.GetAuthenticatedProfileStepConfig[string]{
		StepType:       "ReadLinkedInProfile",
		Annotations:    sdkgo.StepAnnotations{GroupID: "linkedin", GroupLabel: "LinkedIn", Explanation: "Read bounded signup identity claims."},
		Connection:     connection,
		ConnectionName: linkedinConnection.Name,
		MapToOperationInput: func(string) linkedin.GetAuthenticatedProfileInput {
			return linkedin.GetAuthenticatedProfileInput{}
		},
		ProfileLoaded: sdkgo.GoTo(profileTarget{}), VerifiedEmailRequired: sdkgo.GoTo(profileTarget{}),
		InsufficientScope: sdkgo.GoTo(profileTarget{}), AuthorizationRevoked: sdkgo.GoTo(profileTarget{}),
		NotFound: sdkgo.GoTo(profileTarget{}), ProviderRejected: sdkgo.GoTo(profileTarget{}),
		InvalidResponse: sdkgo.GoTo(profileTarget{}), Defect: sdkgo.GoTo(profileTarget{}),
		ResultAttribute: &result,
	})
	require.Equal(t, "ReadLinkedInProfile", step.GetStepType())
}

func TestTypedConnectionAndCredentialsCannotSerializeOrLeak(t *testing.T) {
	encoded, err := json.Marshal(linkedin.Connection{})
	require.ErrorContains(t, err, "cannot be serialized")
	require.Nil(t, encoded)
	credentials := linkedin.Credentials{AccessToken: sdkgo.NewSecretString("secret-token")}
	encoded, err = json.Marshal(credentials)
	require.Error(t, err)
	require.Nil(t, encoded)
	require.NotContains(t, fmt.Sprintf("%#v", credentials), "secret-token")
}
