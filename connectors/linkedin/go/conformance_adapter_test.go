// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package linkedin_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/linkedin/go"
	"github.com/superdurable/dex-connectors-library/connectors/linkedin/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/v2/userinfo"
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	credentials := sdkgo.StaticCredentialProvider[linkedin.Credentials]{
		linkedinConnection: {AccessToken: sdkgo.NewSecretString("one-use-token")},
	}
	if scenario.Name == "missing-credential" {
		credentials = sdkgo.StaticCredentialProvider[linkedin.Credentials]{}
	}
	client, err := linkedin.New(linkedin.Config{UserInfoURL: endpoint}, credentials, linkedin.WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getAuthenticatedProfile": newConformanceQueryInvocation(t, scenario.Input, testsupport.NewDexContext("conformance", "profile-step"), client.GetAuthenticatedProfile(), linkedinConnection),
	}}
}
