// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package drive_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	drive "github.com/superdurable/dex-connectors-library/connectors/google/drive/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath, conformanceFixturePathPrefix = "", ""

type driveConformanceContext struct {
	*dexContext
	attempt       int32
	lastHeartbeat json.RawMessage
}

func (context *driveConformanceContext) Attempt() int32 { return context.attempt }

func (context *driveConformanceContext) RecordHeartbeat(value any) error {
	encoded, err := json.Marshal(value)
	if err == nil {
		context.lastHeartbeat = encoded
	}
	return err
}

func (context *driveConformanceContext) GetLastHeartbeatValue(value any) (bool, error) {
	if len(context.lastHeartbeat) == 0 {
		return false, nil
	}
	return true, json.Unmarshal(context.lastHeartbeat, value)
}

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	client, err := drive.New(drive.Config{Endpoint: endpoint, MaxTextBytes: 8}, sdkgo.StaticCredentialProvider[drive.Credentials]{
		driveConnection: {AccessToken: sdkgo.NewSecretString("drive-token-SENTINEL")},
	}, drive.WithHTTPClient(transport))
	require.NoError(t, err)
	context := &driveConformanceContext{dexContext: newDexContext("conformance")}
	prior := conformancePriorAttempt(t, scenario.PriorHeartbeat, func(raw []byte) { context.lastHeartbeat = raw })
	return &conformanceMappedInvoker{
		t: t, scenario: scenario,
		before: func(attempt int) { context.attempt = prior + int32(attempt) },
		actions: map[string]conformanceOperationInvocation{
			"searchFiles":  newConformanceQueryInvocation(t, scenario.Input, context, client.SearchFiles(), driveConnection),
			"getFile":      newConformanceQueryInvocation(t, scenario.Input, context, client.GetFile(), driveConnection),
			"readFileText": newConformanceQueryInvocation(t, scenario.Input, context, client.ReadFileText(), driveConnection),
			"uploadFile":   newConformanceMutationInvocation(t, scenario.Input, context, client.UploadFile(), driveConnection, scenario.CallID),
		},
	}
}
