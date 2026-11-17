// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package createtopic demonstrates Resend subscription topic creation.
package createtopic

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/resend/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is this example's stable Flow identity.
	FlowType = "ResendCreateTopic"
	// ConnectionName is the static Dex Web connection name.
	ConnectionName = "resend"
)

// Request supplies the new topic settings.
type Request resend.CreateTopicInput

var outcomeAttribute = dex.DefineAttribute[resend.TopicReference]("resend-topic-created")

// Flow creates one Resend subscription topic and completes with its ID.
type Flow struct {
	dex.FlowDefaults
	connection resend.Connection
}

// NewFlow binds a Resend connection.
func NewFlow(connection resend.Connection) *Flow { return &Flow{connection: connection} }

// GetFlowType returns the stable Flow identity.
func (*Flow) GetFlowType() string { return FlowType }

// GetRPCs exposes Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetPersistenceSchema registers the verification result.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{outcomeAttribute}}
}

// GetDexSummary returns the created topic ID.
// dex:field attribute-key:resend-topic-created value-type:json editable:false description:"Created topic"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readTopicOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"resend-topic-created": value}}, nil
}

// GetDexDisplay returns the created topic ID.
// dex:field attribute-key:resend-topic-created value-type:json editable:false description:"Created topic"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readTopicOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"resend-topic-created": value}}, nil
}

func readTopicOutcome(ctx dex.Context) (resend.TopicReference, error) {
	value, err := outcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return resend.TopicReference{}, nil
	}
	return value, err
}

// GetSteps registers the topic creation path.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(prepareTopic{}),
		dex.DefineStep(resend.NewCreateTopicStep(resend.CreateTopicStepConfig[Request]{
			StepType: "CreateTopic", ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "resend-topic", GroupLabel: "Resend topic", Explanation: "Create the selected Resend subscription topic."},
			MapToOperationInput: func(request Request) resend.CreateTopicInput { return resend.CreateTopicInput(request) },
			Created:             sdkgo.GoTo(completeTopic{}),
		})),
		dex.DefineStep(completeTopic{}),
	}
}

// dex:group group-id:resend-topic-prepare group-label:"Prepare topic"
// dex:explanation text:"Pass the typed topic request into the provider create."
type prepareTopic struct{ dex.StepDefaults }

func (prepareTopic) GetStepType() string { return "PrepareTopic" }
func (prepareTopic) WaitFor(dex.Context, Request) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (prepareTopic) Execute(_ dex.Context, request Request) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[Request]("CreateTopic"), request), nil
}

// dex:group group-id:resend-topic-complete group-label:"Complete topic creation"
// dex:explanation text:"Complete after Resend creates the topic."
type completeTopic struct {
	dex.StepDefaultsNoWaitFor[resend.CreateTopicResult]
}

func (completeTopic) GetStepType() string { return "CompleteTopic" }
func (completeTopic) Execute(ctx dex.Context, result resend.CreateTopicResult) (*dex.StepDecision, error) {
	if err := outcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
