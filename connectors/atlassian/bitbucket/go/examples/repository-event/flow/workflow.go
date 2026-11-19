// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package repositoryevent records verified Bitbucket repository webhook events in Dex Flows.
package repositoryevent

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/atlassian/bitbucket/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// ConnectionName is the static Bitbucket connection configured in Dex Web.
	ConnectionName = "bitbucket"
	// TriggerBindingName is the stable repository event binding name.
	TriggerBindingName = "repository-events"
)

var repositoryEventAttribute = dex.DefineAttribute[bitbucket.RepositoryEvent]("bitbucket-repository-event")

// Input carries the verified webhook event and its Trigger identity.
type Input struct {
	// TriggerEventID is the stable ID used to start the Flow.
	TriggerEventID string `json:"triggerEventId"`
	// Event is the verified repository event payload.
	Event bitbucket.RepositoryEvent `json:"event"`
}

// Flow records one verified Bitbucket repository event.
type Flow struct{ dex.FlowDefaults }

// NewFlow constructs the event recording Flow.
func NewFlow() *Flow { return &Flow{} }

// GetFlowType returns the stable event Flow identity.
func (*Flow) GetFlowType() string { return "BitbucketRepositoryEvent" }

// GetPersistenceSchema registers the retained event.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{repositoryEventAttribute}}
}

// GetConnectorTriggerBindings declares the repositoryEventReceived binding for Dex Web.
func (*Flow) GetConnectorTriggerBindings() []sdkgo.TriggerBindingDefinition {
	return []sdkgo.TriggerBindingDefinition{bitbucket.DefineRepositoryEventReceivedTriggerBinding(bitbucket.RepositoryEventReceivedTriggerBindingConfig{
		ConnectionName: ConnectionName, BindingName: TriggerBindingName,
	})}
}

// GetSteps registers the event recording Start Step.
func (*Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{dex.DefineStartStep(recordRepositoryEvent{})}
}

// GetRPCs exposes the recorded event in Dex Web.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:bitbucket-repository-event value-type:json editable:false description:"Verified repository event"
// GetDexSummary returns the recorded event for Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	event, err := readRepositoryEvent(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"bitbucket-repository-event": event}}, nil
}

// dex:field attribute-key:bitbucket-repository-event value-type:json editable:false description:"Verified repository event"
// GetDexDisplay returns the recorded event for Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	event, err := readRepositoryEvent(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"bitbucket-repository-event": event}}, nil
}

func readRepositoryEvent(ctx dex.Context) (bitbucket.RepositoryEvent, error) {
	event, err := repositoryEventAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return bitbucket.RepositoryEvent{}, nil
	}
	return event, err
}

// AcceptEveryEvent admits every supported event; applications can narrow by repository or event key.
func AcceptEveryEvent(sdkgo.TriggerEvent[bitbucket.RepositoryEvent]) bool { return true }

// ResolveFlowID makes a stable Flow ID from the provider request UUID and event key.
func ResolveFlowID(event sdkgo.TriggerEvent[bitbucket.RepositoryEvent]) string {
	digest := sha256.Sum256([]byte(event.ID))
	return "bitbucket-event-" + hex.EncodeToString(digest[:])
}

// MapToFlowInput retains the Trigger ID with the event payload.
func MapToFlowInput(event sdkgo.TriggerEvent[bitbucket.RepositoryEvent]) Input {
	return Input{TriggerEventID: event.ID, Event: event.Payload}
}

// dex:group group-id:event group-label:"Repository event"
// dex:explanation text:"Record the HMAC-verified repository event and complete the Flow."
type recordRepositoryEvent struct{ dex.StepDefaults }

func (recordRepositoryEvent) GetStepType() string { return "RecordRepositoryEvent" }
func (recordRepositoryEvent) WaitFor(dex.Context, Input) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (recordRepositoryEvent) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	if err := repositoryEventAttribute.Set(ctx, input.Event); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(input.Event), nil
}

var _ dex.Flow = (*Flow)(nil)
