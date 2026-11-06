// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package onedrive_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/onedrive/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = "/v1.0"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	client, err := onedrive.New(onedrive.Config{Endpoint: endpoint}, sdkgo.StaticCredentialProvider[onedrive.Credentials]{
		graphConnection: {AccessToken: sdkgo.NewSecretString(graphTestToken)},
	}, onedrive.WithHTTPClient(transport))
	require.NoError(t, err)
	context := newDexContext("conformance")
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"searchFiles":  newConformanceQueryInvocation(t, scenario.Input, context, client.SearchFiles(), graphConnection),
		"getFile":      newConformanceQueryInvocation(t, scenario.Input, context, client.GetFile(), graphConnection),
		"readFileText": newConformanceQueryInvocation(t, scenario.Input, context, client.ReadFileText(), graphConnection),
		"uploadFile":   newConformanceMutationInvocation(t, scenario.Input, context, client.UploadFile(), graphConnection, scenario.CallID),
		"createFolder": newConformanceMutationInvocation(t, scenario.Input, context, client.CreateFolder(), graphConnection, scenario.CallID),
	}}
}
