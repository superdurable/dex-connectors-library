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
	prior := conformancePriorAttempt(t, scenario.PriorHeartbeat, func(raw []byte) { context.previousHeartbeat = raw })
	return &conformanceMappedInvoker{t: t, scenario: scenario,
		before: func(attempt int) {
			if attempt > 1 {
				context.previousHeartbeat = append([]byte(nil), context.recordedHeartbeat...)
			}
			context.attempt = prior + int32(attempt)
		},
		actions: map[string]conformanceOperationInvocation{
			"searchFiles":  newConformanceQueryInvocation(t, scenario.Input, context, client.SearchFiles(), graphConnection),
			"getFile":      newConformanceQueryInvocation(t, scenario.Input, context, client.GetFile(), graphConnection),
			"readFileText": newConformanceQueryInvocation(t, scenario.Input, context, client.ReadFileText(), graphConnection),
			"uploadFile":   newConformanceMutationInvocation(t, scenario.Input, context, client.UploadFile(), graphConnection, scenario.CallID),
			"createFolder": newConformanceMutationInvocation(t, scenario.Input, context, client.CreateFolder(), graphConnection, scenario.CallID),
			"deleteItem":   newConformanceMutationInvocation(t, scenario.Input, context, client.DeleteItem(), graphConnection, scenario.CallID),
			"renameItem":   newConformanceMutationInvocation(t, scenario.Input, context, client.RenameItem(), graphConnection, scenario.CallID),
			"moveItem":     newConformanceMutationInvocation(t, scenario.Input, context, client.MoveItem(), graphConnection, scenario.CallID),
			"copyFile":     newConformanceMutationInvocation(t, scenario.Input, context, client.CopyFile(), graphConnection, scenario.CallID),
		}}
}
