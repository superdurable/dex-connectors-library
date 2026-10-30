// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package connectormock

import (
	"testing"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/internal/conformancefixture"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestConformanceMockDefinitions(t *testing.T) {
	queryDefinition := sdkgo.QueryDefinition{
		Operation: sdkgo.OperationRef{ConnectorID: "sample", OperationID: "getItem"},
		Branches: []sdkgo.BranchDefinition{
			{ID: "found", Description: "item exists"},
			{ID: "missing", Description: "item does not exist", Optional: true},
			{ID: sdkgo.DefectBranchID, Description: "connector defect", Optional: true},
		},
		StepDefaults: sdkgo.StepDefaults{ExecuteMethodTimeout: 15 * time.Second, ExecuteDurability: dex.StepDurabilityAsync},
	}
	mutationDefinition := sdkgo.MutationDefinition{
		Operation: sdkgo.OperationRef{ConnectorID: "sample", OperationID: "createItem"},
		Branches: []sdkgo.BranchDefinition{
			{ID: "created", Description: "item created"},
			{ID: "rejected", Description: "provider rejected the item", Optional: true},
			{ID: sdkgo.UncertainBranchID, Description: "write outcome cannot be confirmed", Optional: true},
			{ID: sdkgo.DefectBranchID, Description: "connector defect", Optional: true},
		},
		StepDefaults: sdkgo.StepDefaults{ExecuteMethodTimeout: 15 * time.Second, ExecuteRetry: sdkgo.DefaultConnectorStepExecuteRetryPolicy(), ExecuteDurability: dex.StepDurabilityAsync},
	}
	query := NewQuery[string, string](t, queryDefinition)
	mutation := NewMutation[string, string](t, mutationDefinition)
	callID := sdkgo.CallID(conformancefixture.ExampleCallID)
	conformancefixture.Check(t, "connectormock.json", map[string]any{
		"queryDefinition":            query.Definition(),
		"mutationDefinition":         mutation.Definition(),
		"recordedCall":               RecordedCall{ConnectorID: "sample", OperationID: "createItem", ConnectionName: "main", FlowID: "flow-42", CallID: callID, Branch: "created"},
		"retryRecordedCall":          RecordedCall{ConnectorID: "sample", OperationID: "createItem", ConnectionName: "main", FlowID: "flow-42", CallID: callID, IsRetry: true},
		"report":                     reportedCall{Connection: "main", Operation: "createItem", Branch: "created", ConnectorID: "sample", FlowID: "flow-42", CallID: callID},
		"reportFileName":             ReportFileName,
		"reportDirectoryEnvironment": ReportDirectoryEnvironment,
	})
}
