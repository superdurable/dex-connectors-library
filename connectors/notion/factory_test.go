// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package notion_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/notion"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type createdPageTarget struct {
	dex.StepDefaultsNoWaitFor[notion.CreatePageResult]
}

func (createdPageTarget) Execute(dex.Context, notion.CreatePageResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(struct{}{}), nil
}

func TestCreatePageFactoryRequiresOnlyTheHappyPath(t *testing.T) {
	connection, err := notion.NewConnection(newNotionClient(t, "http://127.0.0.1:1"), notionConnection)
	require.NoError(t, err)
	annotations := sdkgo.StepAnnotations{GroupID: "notion", GroupLabel: "Notion", Explanation: "Create a row."}
	mapToInput := func(string) notion.CreatePageInput { return notion.CreatePageInput{} }
	require.NotPanics(t, func() {
		notion.NewCreatePageStep(notion.CreatePageStepConfig[string]{
			StepType: "Create", Annotations: annotations, Connection: connection, ConnectionName: notionConnection.Name,
			MapToOperationInput: mapToInput, Created: sdkgo.GoTo(createdPageTarget{}),
		})
	})
	require.Panics(t, func() {
		notion.NewCreatePageStep(notion.CreatePageStepConfig[string]{
			StepType: "Create", Annotations: annotations, Connection: connection,
			MapToOperationInput: mapToInput, Uncertain: sdkgo.GoTo(createdPageTarget{}),
		})
	})
	require.Panics(t, func() {
		notion.NewCreatePageStep(notion.CreatePageStepConfig[string]{
			StepType: "Create", Annotations: annotations, Connection: connection, ConnectionName: "another-connection",
			MapToOperationInput: mapToInput, Created: sdkgo.GoTo(createdPageTarget{}),
		})
	})
}

// TestOnlyTheUnkeyedCreateIsUncertainAndSync records the duplicate-dispatch decision per operation.
func TestOnlyTheUnkeyedCreateIsUncertainAndSync(t *testing.T) {
	for _, test := range []struct {
		operation    string
		branches     []sdkgo.BranchDefinition
		defaults     sdkgo.StepDefaults
		hasUncertain bool
	}{
		{"search", notion.SearchDefinition.Branches, notion.SearchDefinition.StepDefaults, false},
		{"queryDatabase", notion.QueryDatabaseDefinition.Branches, notion.QueryDatabaseDefinition.StepDefaults, false},
		{"getPage", notion.GetPageDefinition.Branches, notion.GetPageDefinition.StepDefaults, false},
		{"createPage", notion.CreatePageDefinition.Branches, notion.CreatePageDefinition.StepDefaults, true},
		{"updatePageProperties", notion.UpdatePagePropertiesDefinition.Branches, notion.UpdatePagePropertiesDefinition.StepDefaults, false},
	} {
		required, hasUncertain := 0, false
		for _, branch := range test.branches {
			if branch.ID == sdkgo.UncertainBranchID {
				hasUncertain = true
			}
			if !branch.Optional {
				required++
			}
		}
		require.Equal(t, 1, required, test.operation)
		require.Equal(t, test.hasUncertain, hasUncertain, test.operation)
		expectedDurability := dex.StepDurabilityAsync
		if test.hasUncertain {
			expectedDurability = dex.StepDurabilitySync
		}
		require.Equal(t, expectedDurability, test.defaults.ExecuteDurability, "%s: an async fallback attempt would resend an unkeyed write", test.operation)
	}
}

// TestWritesWaitForNotionsSlowestDocumentedResponse keeps the Step longer than Notion's roughly
// 55-second write deadline, and the heartbeat timeout no shorter, so a silent request is not abandoned.
func TestWritesWaitForNotionsSlowestDocumentedResponse(t *testing.T) {
	for _, defaults := range []sdkgo.StepDefaults{notion.CreatePageDefinition.StepDefaults, notion.UpdatePagePropertiesDefinition.StepDefaults} {
		require.Equal(t, 90*time.Second, defaults.ExecuteMethodTimeout)
		require.GreaterOrEqual(t, defaults.HeartbeatTimeout, defaults.ExecuteMethodTimeout)
	}
}

func TestNotionConnectionCannotBeSerialized(t *testing.T) {
	connection, err := notion.NewConnection(newNotionClient(t, "http://127.0.0.1:1"), notionConnection)
	require.NoError(t, err)
	_, err = json.Marshal(connection)
	require.Error(t, err)
	require.Equal(t, "notion.Connection{[REDACTED]}", fmt.Sprint(connection))
}
