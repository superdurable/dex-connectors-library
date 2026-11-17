// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package accountdiscovery

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/drip/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// SubscriberPageFlowType is the stable subscriber page example identity.
const SubscriberPageFlowType = "DripSubscriberPage"

var subscriberPageAttribute = dex.DefineAttribute[drip.SubscribersPage]("drip-subscribers-page")

// SubscriberPageFlow reads one bounded page of Drip subscriber identities.
type SubscriberPageFlow struct {
	dex.FlowDefaults
	connection drip.Connection
}

// NewSubscriberPageFlow binds the provider connection at registration time.
func NewSubscriberPageFlow(connection drip.Connection) *SubscriberPageFlow {
	return &SubscriberPageFlow{connection: connection}
}

// GetFlowType preserves the subscriber page Flow identity.
func (*SubscriberPageFlow) GetFlowType() string { return SubscriberPageFlowType }

// GetPersistenceSchema registers the subscriber page Attribute.
func (*SubscriberPageFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{subscriberPageAttribute}}
}

// GetRPCs registers Dex Web summary and display views.
func (flow *SubscriberPageFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:drip-subscribers-page value-type:json editable:false description:"Subscriber page"
// GetDexSummary shows the last subscriber page in Dex Web.
func (*SubscriberPageFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readSubscriberPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"drip-subscribers-page": page}}, nil
}

// dex:field attribute-key:drip-subscribers-page value-type:json editable:false description:"Subscriber page"
// GetDexDisplay shows the last subscriber page in Dex Web.
func (*SubscriberPageFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readSubscriberPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"drip-subscribers-page": page}}, nil
}

func readSubscriberPage(ctx dex.Context) (drip.SubscribersPage, error) {
	page, err := subscriberPageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return drip.SubscribersPage{}, nil
	}
	return page, err
}

// GetSteps registers the operation-specific subscriber page Step.
func (flow *SubscriberPageFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startSubscriberPage{}),
		dex.DefineStep(drip.NewListSubscribersStep(drip.ListSubscribersStepConfig[drip.ListSubscribersInput]{
			StepType: "CallListSubscribers", ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Drip", Explanation: "Read one subscriber page from Drip."},
			Connection:          flow.connection,
			MapToOperationInput: mapSubscriberPageInput,
			Listed:              sdkgo.GoTo(completeSubscriberPage{}),
		})),
		dex.DefineStep(completeSubscriberPage{}),
	}
}

func mapSubscriberPageInput(input drip.ListSubscribersInput) drip.ListSubscribersInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the subscriber page request to Drip."
type startSubscriberPage struct{ dex.StepDefaults }

func (startSubscriberPage) GetStepType() string { return "StartSubscriberPage" }
func (startSubscriberPage) WaitFor(dex.Context, drip.ListSubscribersInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startSubscriberPage) Execute(_ dex.Context, input drip.ListSubscribersInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[drip.ListSubscribersInput]("CallListSubscribers"), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with the selected subscriber page."
type completeSubscriberPage struct {
	dex.StepDefaultsNoWaitFor[drip.ListSubscribersResult]
}

func (completeSubscriberPage) GetStepType() string { return "CompleteSubscriberPage" }
func (completeSubscriberPage) Execute(ctx dex.Context, result drip.ListSubscribersResult) (*dex.StepDecision, error) {
	if err := subscriberPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*SubscriberPageFlow)(nil)
