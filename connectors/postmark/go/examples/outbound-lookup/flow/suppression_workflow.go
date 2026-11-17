// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package outboundlookup

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/postmark/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// SuppressionFlowType identifies the runnable suppression-list Flow.
const SuppressionFlowType = "PostmarkSuppressionList"

const suppressionStepType = "ListSuppressions"

var suppressionPageAttribute = dex.DefineAttribute[postmark.SuppressionPage]("postmark-suppression-page")

// SuppressionRequest is a typed suppression request.
type SuppressionRequest postmark.ListSuppressionsInput

// SuppressionFlow lists suppressed recipients and stores the result.
type SuppressionFlow struct {
	dex.FlowDefaults
	connection postmark.Connection
}

// NewSuppressionFlow binds the configured Postmark connection.
func NewSuppressionFlow(connection postmark.Connection) *SuppressionFlow {
	return &SuppressionFlow{connection: connection}
}

// GetFlowType returns the stable example Flow identity.
func (*SuppressionFlow) GetFlowType() string { return SuppressionFlowType }

// GetSteps defines preparation, provider lookup, and completion Steps.
func (flow *SuppressionFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(prepareSuppressionList{}),
		dex.DefineStep(postmark.NewListSuppressionsStep(postmark.ListSuppressionsStepConfig[SuppressionRequest]{
			StepType: suppressionStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations: sdkgo.StepAnnotations{GroupID: "postmark-suppressions", GroupLabel: "List suppressions", Explanation: "List Postmark suppressed recipients."},
			MapToOperationInput: func(request SuppressionRequest) postmark.ListSuppressionsInput {
				return postmark.ListSuppressionsInput(request)
			},
			Listed: sdkgo.GoTo(completeSuppressionList{}),
		})),
		dex.DefineStep(completeSuppressionList{}),
	}
}

// GetRPCs exposes the suppression page in Dex Web.
func (flow *SuppressionFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetPersistenceSchema registers the stored suppression page.
func (*SuppressionFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{suppressionPageAttribute}}
}

// GetDexSummary returns the suppression page.
// dex:field attribute-key:postmark-suppression-page value-type:json editable:false description:"Postmark suppression page"
func (*SuppressionFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readSuppressionPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"postmark-suppression-page": page}}, nil
}

// GetDexDisplay returns the suppression page.
// dex:field attribute-key:postmark-suppression-page value-type:json editable:false description:"Postmark suppression page"
func (*SuppressionFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readSuppressionPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"postmark-suppression-page": page}}, nil
}

func readSuppressionPage(ctx dex.Context) (postmark.SuppressionPage, error) {
	page, err := suppressionPageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return postmark.SuppressionPage{}, nil
	}
	return page, err
}

// dex:group group-id:postmark-suppression-prepare group-label:"Prepare suppression list"
// dex:explanation text:"Pass the requested stream and filter to the provider query."
type prepareSuppressionList struct{ dex.StepDefaults }

func (prepareSuppressionList) GetStepType() string { return "PrepareSuppressionList" }
func (prepareSuppressionList) WaitFor(dex.Context, SuppressionRequest) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (prepareSuppressionList) Execute(_ dex.Context, request SuppressionRequest) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[SuppressionRequest](suppressionStepType), request), nil
}

// dex:group group-id:postmark-suppression-complete group-label:"Complete suppression list"
// dex:explanation text:"Persist the page and complete the Flow."
type completeSuppressionList struct {
	dex.StepDefaultsNoWaitFor[postmark.ListSuppressionsResult]
}

func (completeSuppressionList) GetStepType() string { return "CompleteSuppressionList" }
func (completeSuppressionList) Execute(ctx dex.Context, result postmark.ListSuppressionsResult) (*dex.StepDecision, error) {
	if err := suppressionPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*SuppressionFlow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*SuppressionFlow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*SuppressionFlow)(nil).GetDexDisplay
