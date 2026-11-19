// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package generatesummary

import (
	gemini "github.com/superdurable/dex-connectors-library/connectors/google/gemini/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const inventoryFlowType = "GeminiFileSearchStoreInventory"
const listStoresStepType = "ListFileSearchStores"

var inventoryPageAttribute = dex.DefineAttribute[gemini.FileSearchStoresPage]("gemini-file-search-stores-page")

// StoreInventoryRequest selects one bounded page of Gemini File Search stores.
type StoreInventoryRequest struct {
	// PageSize is 1 through 20; zero uses the provider default.
	PageSize int `json:"pageSize,omitempty"`
	// PageToken continues a previous page; blank starts at the beginning.
	PageToken string `json:"pageToken,omitempty"`
}

// StoreInventoryFlow lists File Search stores through a typed Connector Query.
type StoreInventoryFlow struct {
	dex.FlowDefaults
	connection gemini.Connection
}

// NewStoreInventoryFlow binds the example's existing Gemini connection.
func NewStoreInventoryFlow(connection gemini.Connection) *StoreInventoryFlow {
	return &StoreInventoryFlow{connection: connection}
}

// GetFlowType keeps the inventory Flow's durable identity stable.
func (*StoreInventoryFlow) GetFlowType() string { return inventoryFlowType }

// GetSteps registers the start, provider Query, and completion Steps.
func (flow *StoreInventoryFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startStoreInventory{}),
		dex.DefineStep(gemini.NewListFileSearchStoresStep(gemini.ListFileSearchStoresStepConfig[StoreInventoryRequest]{
			StepType: listStoresStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "gemini", GroupLabel: "Gemini",
				Explanation: "List one page of Gemini File Search stores.",
			},
			Connection: flow.connection,
			MapToOperationInput: func(request StoreInventoryRequest) gemini.ListFileSearchStoresInput {
				return gemini.ListFileSearchStoresInput{PageSize: request.PageSize, PageToken: request.PageToken}
			},
			Listed: sdkgo.GoTo(completeStoreInventory{}),
		})),
		dex.DefineStep(completeStoreInventory{}),
	}
}

// GetRPCs exposes the page in Dex Web after completion.
func (flow *StoreInventoryFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetPersistenceSchema registers the page Attribute consumed by the view RPCs.
func (*StoreInventoryFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{inventoryPageAttribute}}
}

// dex:field attribute-key:gemini-file-search-stores-page value-type:json editable:false description:"File Search stores page"
// GetDexSummary returns the last listed page for Dex Web's summary view.
func (*StoreInventoryFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := optionalAttribute(ctx, inventoryPageAttribute)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"gemini-file-search-stores-page": page}}, nil
}

// dex:field attribute-key:gemini-file-search-stores-page value-type:json editable:false description:"Store names and next page token"
// GetDexDisplay returns the last listed page for Dex Web's display view.
func (*StoreInventoryFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := optionalAttribute(ctx, inventoryPageAttribute)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"gemini-file-search-stores-page": page}}, nil
}

// dex:group group-id:gemini group-label:"Gemini"
// dex:explanation text:"Accept an inventory request and ask Gemini for its File Search stores."
type startStoreInventory struct{ dex.StepDefaults }

func (startStoreInventory) GetStepType() string { return "StartStoreInventory" }
func (startStoreInventory) WaitFor(dex.Context, StoreInventoryRequest) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startStoreInventory) Execute(_ dex.Context, request StoreInventoryRequest) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[StoreInventoryRequest](listStoresStepType), request), nil
}

// dex:group group-id:gemini group-label:"Gemini"
// dex:explanation text:"Store the returned File Search store page and complete the Flow."
type completeStoreInventory struct {
	dex.StepDefaultsNoWaitFor[gemini.ListFileSearchStoresResult]
}

func (completeStoreInventory) GetStepType() string { return "CompleteStoreInventory" }
func (completeStoreInventory) Execute(ctx dex.Context, result gemini.ListFileSearchStoresResult) (*dex.StepDecision, error) {
	if err := inventoryPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}
