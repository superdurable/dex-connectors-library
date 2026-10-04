// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package confluence_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/confluence"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type createdPageTarget struct {
	dex.StepDefaultsNoWaitFor[confluence.CreatePageResult]
}

func (createdPageTarget) Execute(dex.Context, confluence.CreatePageResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(struct{}{}), nil
}

func TestCreatePageFactoryRequiresOnlyTheHappyPath(t *testing.T) {
	connection, err := confluence.NewConnection(newConfluenceClient(t, "http://127.0.0.1:1"), confluenceConnection)
	require.NoError(t, err)
	annotations := sdkgo.StepAnnotations{GroupID: "confluence", GroupLabel: "Confluence", Explanation: "Publish a page."}
	mapToInput := func(string) confluence.CreatePageInput { return confluence.CreatePageInput{} }
	require.NotPanics(t, func() {
		confluence.NewCreatePageStep(confluence.CreatePageStepConfig[string]{
			StepType: "Create", Annotations: annotations, Connection: connection, ConnectionName: confluenceConnection.Name,
			MapToOperationInput: mapToInput, Created: sdkgo.GoTo(createdPageTarget{}),
		})
	})
	require.Panics(t, func() {
		confluence.NewCreatePageStep(confluence.CreatePageStepConfig[string]{
			StepType: "Create", Annotations: annotations, Connection: connection, ConnectionName: confluenceConnection.Name,
			MapToOperationInput: mapToInput, TitleConflict: sdkgo.GoTo(createdPageTarget{}),
		})
	})
	require.Panics(t, func() {
		confluence.NewCreatePageStep(confluence.CreatePageStepConfig[string]{
			StepType: "Create", Annotations: annotations, Connection: connection, ConnectionName: "another-connection",
			MapToOperationInput: mapToInput, Created: sdkgo.GoTo(createdPageTarget{}),
		})
	})
}

// TestDuplicateDispatchDecisionsPerOperation records how each write stays safe under a repeated dispatch.
func TestDuplicateDispatchDecisionsPerOperation(t *testing.T) {
	for _, test := range []struct {
		operation    string
		branches     []sdkgo.BranchDefinition
		defaults     sdkgo.StepDefaults
		hasUncertain bool
		durability   dex.StepDurability
	}{
		{"searchPages", confluence.SearchPagesDefinition.Branches, confluence.SearchPagesDefinition.StepDefaults, false, dex.StepDurabilityAsync},
		{"getPage", confluence.GetPageDefinition.Branches, confluence.GetPageDefinition.StepDefaults, false, dex.StepDurabilityAsync},
		// Title uniqueness makes a repeat safe; sync keeps a backup attempt from reporting its own page as a conflict.
		{"createPage", confluence.CreatePageDefinition.Branches, confluence.CreatePageDefinition.StepDefaults, false, dex.StepDurabilitySync},
		// The version number is accepted once, so a backup attempt finds its own version.
		{"updatePage", confluence.UpdatePageDefinition.Branches, confluence.UpdatePageDefinition.StepDefaults, false, dex.StepDurabilityAsync},
		// Only the heartbeat checkpoint prevents a second comment, and a backup attempt cannot see it.
		{"addComment", confluence.AddCommentDefinition.Branches, confluence.AddCommentDefinition.StepDefaults, true, dex.StepDurabilitySync},
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
		require.Equal(t, test.durability, test.defaults.ExecuteDurability, test.operation)
	}
}

func TestConfluenceConnectionCannotBeSerialized(t *testing.T) {
	connection, err := confluence.NewConnection(newConfluenceClient(t, "http://127.0.0.1:1"), confluenceConnection)
	require.NoError(t, err)
	_, err = json.Marshal(connection)
	require.Error(t, err)
	require.Equal(t, "confluence.Connection{[REDACTED]}", fmt.Sprint(connection))
}
