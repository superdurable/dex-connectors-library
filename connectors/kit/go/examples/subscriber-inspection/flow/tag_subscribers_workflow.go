// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package subscriberinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/kit/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// TagSubscribersFlowType identifies the tagged-subscriber example Flow.
const TagSubscribersFlowType = "KitTagSubscribersInspection"

const listTagSubscribersStepType = "CallListTagSubscribers"

var tagSubscribersPageAttribute = dex.DefineAttribute[kit.SubscribersPage]("kit-tag-subscribers-page")

// TagSubscribersFlow lists one page of Kit tagSubscribers.
type TagSubscribersFlow struct {
	dex.FlowDefaults
	connection kit.Connection
}

// NewTagSubscribersFlow binds the Kit connection at registration time.
func NewTagSubscribersFlow(connection kit.Connection) *TagSubscribersFlow {
	return &TagSubscribersFlow{connection: connection}
}

func (*TagSubscribersFlow) GetFlowType() string { return TagSubscribersFlowType }

func (*TagSubscribersFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{tagSubscribersPageAttribute}}
}

func (flow *TagSubscribersFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startTagSubscribers{}),
		dex.DefineStep(kit.NewListTagSubscribersStep(kit.ListTagSubscribersStepConfig[kit.ListTagSubscribersInput]{
			StepType: listTagSubscribersStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Kit", Explanation: "List one page of tagSubscribers."},
			Connection:  flow.connection, MapToOperationInput: MapToListTagSubscribersInput,
			Listed: sdkgo.GoTo(completeTagSubscribers{}),
		})),
		dex.DefineStep(completeTagSubscribers{}),
	}
}

func (flow *TagSubscribersFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:kit-tag-subscribers-page value-type:json editable:false description:"Tagged subscriber page"
// GetDexSummary shows the tagSubscriber page in Dex Web.
func (*TagSubscribersFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readSubscribersPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"kit-tag-subscribers-page": page}}, nil
}

// dex:field attribute-key:kit-tag-subscribers-page value-type:json editable:false description:"Tagged subscriber page"
// GetDexDisplay shows the tagSubscriber page in Dex Web.
func (*TagSubscribersFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readSubscribersPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"kit-tag-subscribers-page": page}}, nil
}

func readSubscribersPage(ctx dex.Context) (kit.SubscribersPage, error) {
	page, err := tagSubscribersPageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return kit.SubscribersPage{}, nil
	}
	return page, err
}

// MapToListTagSubscribersInput passes the typed page request to Kit.
func MapToListTagSubscribersInput(input kit.ListTagSubscribersInput) kit.ListTagSubscribersInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the requested tagSubscriber page to the provider Step."
type startTagSubscribers struct{ dex.StepDefaults }

func (startTagSubscribers) GetStepType() string { return "StartListTagSubscribers" }
func (startTagSubscribers) WaitFor(dex.Context, kit.ListTagSubscribersInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startTagSubscribers) Execute(_ dex.Context, input kit.ListTagSubscribersInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[kit.ListTagSubscribersInput](listTagSubscribersStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with one typed tagSubscriber page."
type completeTagSubscribers struct {
	dex.StepDefaultsNoWaitFor[kit.ListTagSubscribersResult]
}

func (completeTagSubscribers) GetStepType() string { return "CompleteListTagSubscribers" }
func (completeTagSubscribers) Execute(ctx dex.Context, result kit.ListTagSubscribersResult) (*dex.StepDecision, error) {
	if err := tagSubscribersPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*TagSubscribersFlow)(nil)
