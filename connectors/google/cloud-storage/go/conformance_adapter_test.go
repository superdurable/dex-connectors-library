// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package cloudstorage

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "google-cloud-storage", Name: "google-cloud-storage"}
	credentials := Credentials{
		OAuthClientID: "test-client", OAuthClientSecret: sdkgo.NewSecretString("test-client-secret"),
		AccessToken: sdkgo.NewSecretString("test-storage-token"), RefreshToken: sdkgo.NewSecretString("test-refresh-token"),
	}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{reference: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	context := singledispatchtest.NewContext("storage-conformance", "provider-step")
	return &conformanceMappedInvoker{t: t, scenario: scenario,
		before: func(attempt int) {
			if attempt > 1 { context.AdvanceAttempt() }
		},
		actions: map[string]conformanceOperationInvocation{
			"listObjects": newConformanceQueryInvocation(t, scenario.Input, context, client.ListObjects(), reference),
			"downloadObject": newConformanceQueryInvocation(t, scenario.Input, context, client.DownloadObject(), reference),
			"uploadObject": newConformanceMutationInvocation(t, scenario.Input, context, client.UploadObject(), reference, scenario.CallID),
		},
	}
}
