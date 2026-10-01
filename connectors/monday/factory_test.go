// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package monday_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/monday"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// mondayIdempotencyCacheDuration is how long monday.com replays a cached mutation result for one key.
const mondayIdempotencyCacheDuration = 30 * time.Minute

type completeTarget[IN any] struct {
	dex.StepDefaultsNoWaitFor[IN]
}

func (completeTarget[IN]) Execute(dex.Context, IN) (*dex.StepDecision, error) {
	return dex.GracefulComplete(struct{}{}), nil
}

func TestFactoriesRequireOnlyTheHappyPathBranch(t *testing.T) {
	connection := newTestConnection(t)
	annotations := sdkgo.StepAnnotations{GroupID: "monday", GroupLabel: "monday.com", Explanation: "Call monday.com."}
	require.NotPanics(t, func() {
		monday.NewListItemsStep(monday.ListItemsStepConfig[string]{
			StepType: "ListItems", Annotations: annotations, Connection: connection, ConnectionName: mondayConnection.Name,
			MapToOperationInput: func(boardID string) monday.ListItemsInput { return monday.ListItemsInput{BoardID: boardID} },
			Listed:              sdkgo.GoTo(completeTarget[monday.ListItemsResult]{}),
		})
		monday.NewGetItemStep(monday.GetItemStepConfig[string]{
			StepType: "GetItem", Annotations: annotations, Connection: connection,
			MapToOperationInput: func(itemID string) monday.GetItemInput { return monday.GetItemInput{ItemID: itemID} },
			Found:               sdkgo.GoTo(completeTarget[monday.GetItemResult]{}),
		})
		monday.NewCreateItemStep(monday.CreateItemStepConfig[string]{
			StepType: "CreateItem", Annotations: annotations, Connection: connection,
			MapToOperationInput: func(string) monday.CreateItemInput { return validCreateItemInput() },
			Created:             sdkgo.GoTo(completeTarget[monday.CreateItemResult]{}),
		})
		monday.NewUpdateItemColumnValuesStep(monday.UpdateItemColumnValuesStepConfig[string]{
			StepType: "UpdateItem", Annotations: annotations, Connection: connection,
			MapToOperationInput: func(itemID string) monday.UpdateItemColumnValuesInput {
				return monday.UpdateItemColumnValuesInput{BoardID: testBoardID, ItemID: itemID, ColumnValues: map[string]monday.ColumnValue{"status": monday.StatusLabelValue("Done")}}
			},
			Updated: sdkgo.GoTo(completeTarget[monday.UpdateItemColumnValuesResult]{}),
		})
		monday.NewAddUpdateStep(monday.AddUpdateStepConfig[string]{
			StepType: "AddUpdate", Annotations: annotations, Connection: connection,
			MapToOperationInput: func(itemID string) monday.AddUpdateInput { return monday.AddUpdateInput{ItemID: itemID, Body: "Done."} },
			Added:               sdkgo.GoTo(completeTarget[monday.AddUpdateResult]{}),
		})
	})
	require.Panics(t, func() {
		monday.NewAddUpdateStep(monday.AddUpdateStepConfig[string]{
			StepType: "AddUpdate", Connection: connection,
			MapToOperationInput: func(itemID string) monday.AddUpdateInput { return monday.AddUpdateInput{ItemID: itemID, Body: "Done."} },
			Uncertain:           sdkgo.GoTo(completeTarget[monday.AddUpdateResult]{}),
		})
	}, "added is the required branch")
	require.Panics(t, func() {
		monday.NewGetItemStep(monday.GetItemStepConfig[string]{
			StepType: "GetItem", Connection: connection, ConnectionName: "another-connection",
			MapToOperationInput: func(itemID string) monday.GetItemInput { return monday.GetItemInput{ItemID: itemID} },
			Found:               sdkgo.GoTo(completeTarget[monday.GetItemResult]{}),
		})
	}, "the static ConnectionName must match the runtime connection")
}

func TestDefinitionsDeclareOptionalBranchesAndKeyedAsyncDurability(t *testing.T) {
	require.Equal(t, map[sdkgo.BranchID]bool{"listed": false, "notFound": true, "providerRejected": true, "invalidResponse": true, "defect": true},
		branchOptionality(monday.ListItemsDefinition.Branches))
	require.Equal(t, map[sdkgo.BranchID]bool{"found": false, "notFound": true, "providerRejected": true, "invalidResponse": true, "defect": true},
		branchOptionality(monday.GetItemDefinition.Branches))
	require.Equal(t, map[sdkgo.BranchID]bool{"created": false, "notFound": true, "providerRejected": true, "uncertain": true, "defect": true},
		branchOptionality(monday.CreateItemDefinition.Branches), "uncertain covers only an accepted create whose answer is unusable")
	require.Equal(t, map[sdkgo.BranchID]bool{"updated": false, "notFound": true, "providerRejected": true, "invalidResponse": true, "defect": true},
		branchOptionality(monday.UpdateItemColumnValuesDefinition.Branches), "an absolute-value update is safe to repeat, so it has no uncertain branch")
	require.Equal(t, map[sdkgo.BranchID]bool{"added": false, "notFound": true, "providerRejected": true, "uncertain": true, "defect": true},
		branchOptionality(monday.AddUpdateDefinition.Branches))
	for name, defaults := range map[string]sdkgo.StepDefaults{
		"listItems": monday.ListItemsDefinition.StepDefaults, "getItem": monday.GetItemDefinition.StepDefaults,
		"createItem": monday.CreateItemDefinition.StepDefaults, "updateItemColumnValues": monday.UpdateItemColumnValuesDefinition.StepDefaults,
		"addUpdate": monday.AddUpdateDefinition.StepDefaults,
	} {
		require.Equal(t, dex.StepDurabilityAsync, defaults.ExecuteDurability, "%s: a duplicate dispatch replays under the idempotency key", name)
		require.Equal(t, 30*time.Second, defaults.ExecuteMethodTimeout, name)
		require.GreaterOrEqual(t, defaults.ExecuteRetry.TotalDuration, 2*time.Minute, "%s: a one-minute complexity reset fits in the window", name)
		require.Less(t, defaults.ExecuteRetry.TotalDuration, mondayIdempotencyCacheDuration,
			"%s: every retry must reach monday.com while it still caches the first attempt's result", name)
	}
}

func TestConnectionAndCredentialsNeverRevealSecrets(t *testing.T) {
	connection := newTestConnection(t)
	_, err := json.Marshal(connection)
	require.Error(t, err)
	require.NotContains(t, fmt.Sprintf("%v %#v", connection, connection), testAPIToken)
	credentials := monday.Credentials{AuthMethodID: monday.OAuthAuthMethodID, AccessToken: sdkgo.NewSecretString(testAPIToken), RefreshToken: sdkgo.NewSecretString(testAPIToken)}
	rendered := fmt.Sprintf("%v %+v %#v", credentials, credentials, credentials)
	require.NotContains(t, rendered, testAPIToken)
	_, err = json.Marshal(credentials)
	require.Error(t, err)
}

func branchOptionality(definitions []sdkgo.BranchDefinition) map[sdkgo.BranchID]bool {
	branches := map[sdkgo.BranchID]bool{}
	for _, branch := range definitions {
		branches[branch.ID] = branch.Optional
	}
	return branches
}

func newTestConnection(t *testing.T) monday.Connection {
	t.Helper()
	client, err := monday.New(monday.Config{}, testCredentialProvider())
	require.NoError(t, err)
	connection, err := monday.NewConnection(client, mondayConnection)
	require.NoError(t, err)
	return connection
}
