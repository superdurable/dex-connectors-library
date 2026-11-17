// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package smartsheet

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/2.0"
const conformanceFixturePathPrefix = ""

var conformanceConnection = sdkgo.ConnectionRef{Provider: "smartsheet", Name: "smartsheet"}

type smartsheetConformanceContext struct {
	queryContext
	attempt  int32
	prior    json.RawMessage
	recorded json.RawMessage
}

func (context *smartsheetConformanceContext) Attempt() int32 { return context.attempt }
func (context *smartsheetConformanceContext) RecordHeartbeat(value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	context.recorded = encoded
	return nil
}
func (context *smartsheetConformanceContext) GetLastHeartbeatValue(value any) (bool, error) {
	if len(context.prior) == 0 {
		return false, nil
	}
	return true, json.Unmarshal(context.prior, value)
}

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		conformanceConnection: {APIKey: sdkgo.NewSecretString(testAPIKey)},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	context := &smartsheetConformanceContext{attempt: 1}
	if len(scenario.PriorHeartbeat) > 0 {
		context.prior = append(json.RawMessage(nil), scenario.PriorHeartbeat...)
		context.attempt = conformancePriorAttempt(t, scenario.PriorHeartbeat, func(value []byte) { context.prior = value }) + 1
	}
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if attempt > 1 {
			context.prior = append(json.RawMessage(nil), context.recorded...)
			context.recorded = nil
			context.attempt = int32(attempt)
		}
	}, actions: map[string]conformanceOperationInvocation{
		"listSheets":              newConformanceQueryInvocation(t, scenario.Input, context, client.ListSheets(), conformanceConnection),
		"getSheet":                newConformanceQueryInvocation(t, scenario.Input, context, client.GetSheet(), conformanceConnection),
		"getRow":                  newConformanceQueryInvocation(t, scenario.Input, context, client.GetRow(), conformanceConnection),
		"addRow":                  newConformanceMutationInvocation(t, scenario.Input, context, client.AddRow(), conformanceConnection, scenario.CallID),
		"updateRow":               newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateRow(), conformanceConnection, scenario.CallID),
		"deleteRow":               newConformanceMutationInvocation(t, scenario.Input, context, client.DeleteRow(), conformanceConnection, scenario.CallID),
		"createSheetInWorkspace":  newConformanceMutationInvocation(t, scenario.Input, context, client.CreateSheetInWorkspace(), conformanceConnection, scenario.CallID),
		"createSheetInFolder":     newConformanceMutationInvocation(t, scenario.Input, context, client.CreateSheetInFolder(), conformanceConnection, scenario.CallID),
		"updateSheet":             newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateSheet(), conformanceConnection, scenario.CallID),
		"deleteSheet":             newConformanceMutationInvocation(t, scenario.Input, context, client.DeleteSheet(), conformanceConnection, scenario.CallID),
		"listColumns":             newConformanceQueryInvocation(t, scenario.Input, context, client.ListColumns(), conformanceConnection),
		"getColumn":               newConformanceQueryInvocation(t, scenario.Input, context, client.GetColumn(), conformanceConnection),
		"addColumn":               newConformanceMutationInvocation(t, scenario.Input, context, client.AddColumn(), conformanceConnection, scenario.CallID),
		"updateColumn":            newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateColumn(), conformanceConnection, scenario.CallID),
		"deleteColumn":            newConformanceMutationInvocation(t, scenario.Input, context, client.DeleteColumn(), conformanceConnection, scenario.CallID),
		"getFolder":               newConformanceQueryInvocation(t, scenario.Input, context, client.GetFolder(), conformanceConnection),
		"createFolderInWorkspace": newConformanceMutationInvocation(t, scenario.Input, context, client.CreateFolderInWorkspace(), conformanceConnection, scenario.CallID),
		"createFolderInFolder":    newConformanceMutationInvocation(t, scenario.Input, context, client.CreateFolderInFolder(), conformanceConnection, scenario.CallID),
		"updateFolder":            newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateFolder(), conformanceConnection, scenario.CallID),
		"deleteFolder":            newConformanceMutationInvocation(t, scenario.Input, context, client.DeleteFolder(), conformanceConnection, scenario.CallID),
		"listWorkspaces":          newConformanceQueryInvocation(t, scenario.Input, context, client.ListWorkspaces(), conformanceConnection),
		"getWorkspace":            newConformanceQueryInvocation(t, scenario.Input, context, client.GetWorkspace(), conformanceConnection),
		"createWorkspace":         newConformanceMutationInvocation(t, scenario.Input, context, client.CreateWorkspace(), conformanceConnection, scenario.CallID),
		"updateWorkspace":         newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateWorkspace(), conformanceConnection, scenario.CallID),
		"deleteWorkspace":         newConformanceMutationInvocation(t, scenario.Input, context, client.DeleteWorkspace(), conformanceConnection, scenario.CallID),
	}}
}
