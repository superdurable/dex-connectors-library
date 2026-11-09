// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package topics runs Google PubSub listTopics as one Dex Flow.
package topics

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/google/pubsub/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "GooglePubSubTopicsList"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "google-pubsub"
	operationStepType = "CallListTopics"
)

var topicsAttribute = dex.DefineAttribute[pubsub.TopicsPage]("google-pubsub-page")

// Flow runs ListTopics once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection pubsub.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection pubsub.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{topicsAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(pubsub.NewListTopicsStep(pubsub.ListTopicsStepConfig[pubsub.ListTopicsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Google PubSub", Explanation: "List topics visible in the selected Google Cloud project."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Listed:              sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:google-pubsub-page value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readTopics(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"google-pubsub-page": outcome}}, nil
}

// dex:field attribute-key:google-pubsub-page value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readTopics(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"google-pubsub-page": outcome}}, nil
}

func readTopics(ctx dex.Context) (pubsub.TopicsPage, error) {
	outcome, err := topicsAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return pubsub.TopicsPage{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input pubsub.ListTopicsInput) pubsub.ListTopicsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListTopics" }
func (startRequest) WaitFor(dex.Context, pubsub.ListTopicsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input pubsub.ListTopicsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[pubsub.ListTopicsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[pubsub.ListTopicsResult]
}

func (completeResult) GetStepType() string { return "CompleteListTopics" }
func (completeResult) Execute(ctx dex.Context, result pubsub.ListTopicsResult) (*dex.StepDecision, error) {
	if err := topicsAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
