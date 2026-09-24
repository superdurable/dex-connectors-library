//go:build live

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package linkedinconnector_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	linkedinconnector "github.com/superdurable/dex-connectors-library/connectors/linkedin"
	"github.com/superdurable/dex-connectors-library/connectors/linkedin/internal/testsupport"
	connector "github.com/superdurable/dex-connectors-library/sdk/go"
)

func TestLiveAuthenticatedProfile(t *testing.T) {
	token := os.Getenv("LINKEDIN_CONNECTOR_TEST_TOKEN")
	if token == "" {
		t.Skip("LINKEDIN_CONNECTOR_TEST_TOKEN is not configured")
	}
	client, err := linkedinconnector.New(linkedinconnector.Config{}, connector.StaticCredentialProvider[linkedinconnector.Credentials]{
		linkedinConnection: {AccessToken: connector.NewSecretString(token)},
	})
	require.NoError(t, err)
	profile, err := connector.RunQuery(
		testsupport.NewDexContext("live-linkedin-flow", "live-profile-step"), client.GetAuthenticatedProfile(), linkedinConnection,
		linkedinconnector.GetAuthenticatedProfileInput{},
	)
	require.NoError(t, err)
	require.Equal(t, linkedinconnector.GetAuthenticatedProfileBranchProfileLoaded, profile.Branch)
	require.NotEmpty(t, profile.Value.Subject)
	require.NotEmpty(t, profile.Value.VerifiedEmail)
}
