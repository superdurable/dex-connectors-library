// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package usersearch

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/amplitude/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// UnsetPropertyFlowType is the stable Dex Web Start Flow identity for one user property.
	UnsetPropertyFlowType = "AmplitudeUnsetUserProperty"
	unsetPropertyStepType = "CallUnsetUserProperty"
)

var unsetPropertyAttribute = dex.DefineAttribute[amplitude.IdentifiedUser]("amplitude-unset-property")

// UnsetPropertyFlow removes one user property through the example's Amplitude connection.
type UnsetPropertyFlow struct {
	dex.FlowDefaults
	connection amplitude.Connection
}

// NewUnsetPropertyFlow binds the provider connection at registration time.
func NewUnsetPropertyFlow(connection amplitude.Connection) *UnsetPropertyFlow {
	return &UnsetPropertyFlow{connection: connection}
}

// GetFlowType returns the stable property removal Flow identity.
func (*UnsetPropertyFlow) GetFlowType() string { return UnsetPropertyFlowType }

// GetPersistenceSchema registers the removed property identity.
func (*UnsetPropertyFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{unsetPropertyAttribute}}
}

// GetRPCs exposes read-only Dex Web views of the removed property.
func (flow *UnsetPropertyFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary returns the removed property identity when this Flow has one.
//
// dex:field attribute-key:amplitude-unset-property value-type:json editable:false description:"Removed Amplitude property"
func (*UnsetPropertyFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	property, err := readUnsetProperty(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"amplitude-unset-property": property}}, nil
}

// GetDexDisplay returns the removed property identity when this Flow has one.
//
// dex:field attribute-key:amplitude-unset-property value-type:json editable:false description:"Removed Amplitude property"
func (*UnsetPropertyFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	property, err := readUnsetProperty(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"amplitude-unset-property": property}}, nil
}

func readUnsetProperty(ctx dex.Context) (amplitude.IdentifiedUser, error) {
	property, err := unsetPropertyAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return amplitude.IdentifiedUser{}, nil
	}
	return property, err
}

// GetSteps registers the start, provider unset, and completion Steps.
func (flow *UnsetPropertyFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startPropertyUnset{}),
		dex.DefineStep(amplitude.NewUnsetUserPropertyStep(amplitude.UnsetUserPropertyStepConfig[amplitude.UnsetUserPropertyInput]{
			StepType: unsetPropertyStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Amplitude", Explanation: "Remove one user property from future Amplitude events."},
			Connection:  flow.connection, MapToOperationInput: mapToUnsetPropertyInput,
			Unset: sdkgo.GoTo(completePropertyUnset{}),
		})),
		dex.DefineStep(completePropertyUnset{}),
	}
}

func mapToUnsetPropertyInput(input amplitude.UnsetUserPropertyInput) amplitude.UnsetUserPropertyInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the user and property name to the Amplitude provider Step."
type startPropertyUnset struct{ dex.StepDefaults }

func (startPropertyUnset) GetStepType() string { return "StartUnsetUserProperty" }
func (startPropertyUnset) WaitFor(dex.Context, amplitude.UnsetUserPropertyInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startPropertyUnset) Execute(_ dex.Context, input amplitude.UnsetUserPropertyInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[amplitude.UnsetUserPropertyInput](unsetPropertyStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the removed property identity."
type completePropertyUnset struct {
	dex.StepDefaultsNoWaitFor[amplitude.UnsetUserPropertyResult]
}

func (completePropertyUnset) GetStepType() string { return "CompleteUnsetUserProperty" }
func (completePropertyUnset) Execute(ctx dex.Context, result amplitude.UnsetUserPropertyResult) (*dex.StepDecision, error) {
	if err := unsetPropertyAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*UnsetPropertyFlow)(nil)
