//go:build live

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package linkedin_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/linkedin"
	"github.com/superdurable/dex-connectors-library/connectors/linkedin/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestLiveAuthenticatedProfile(t *testing.T) {
	token := os.Getenv("LINKEDIN_CONNECTOR_TEST_TOKEN")
	if token == "" {
		t.Skip("LINKEDIN_CONNECTOR_TEST_TOKEN is not configured")
	}
	client, err := linkedin.New(linkedin.Config{}, sdkgo.StaticCredentialProvider[linkedin.Credentials]{
		linkedinConnection: {AccessToken: sdkgo.NewSecretString(token)},
	})
	require.NoError(t, err)
	profile, err := sdkgo.RunQuery(
		testsupport.NewDexContext("live-linkedin-flow", "live-profile-step"), client.GetAuthenticatedProfile(), linkedinConnection,
		linkedin.GetAuthenticatedProfileInput{},
	)
	require.NoError(t, err)
	require.Equal(t, linkedin.GetAuthenticatedProfileBranchProfileLoaded, profile.Branch)
	require.NotEmpty(t, profile.Value.Subject)
	require.NotEmpty(t, profile.Value.VerifiedEmail)
}
