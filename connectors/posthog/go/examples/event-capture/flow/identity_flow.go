// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package eventcapture

import (
	"errors"
	"time"

	"github.com/superdurable/dex-connectors-library/connectors/posthog/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const identityStepType = "IdentifyPostHogPerson"

var identityOutcomeAttribute = dex.DefineAttribute[posthog.CapturedEvent]("posthog-identity-outcome")

// IdentityFlow sends one identified person-property update to PostHog.
type IdentityFlow struct {
	dex.FlowDefaults
	connection posthog.Connection
}

// NewIdentityFlow binds the configured PostHog connection.
func NewIdentityFlow(connection posthog.Connection) *IdentityFlow {
	return &IdentityFlow{connection: connection}
}

// GetFlowType preserves the example's stable identity.
func (*IdentityFlow) GetFlowType() string { return "PostHogIdentifyPerson" }

// GetPersistenceSchema registers the result Attribute.
func (*IdentityFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{identityOutcomeAttribute}}
}

// GetSteps registers the provider Step and completion path.
func (flow *IdentityFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startIdentity{}),
		dex.DefineStep(posthog.NewIdentifyPersonStep(posthog.IdentifyPersonStepConfig[posthog.IdentifyPersonInput]{
			StepType: identityStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "person", GroupLabel: "Person", Explanation: "Associate non-secret properties with one PostHog person."},
			MapToOperationInput: func(input posthog.IdentifyPersonInput) posthog.IdentifyPersonInput { return input },
			Accepted:            sdkgo.GoTo(acceptedIdentity{}),
		})),
		dex.DefineStep(acceptedIdentity{}),
	}
}

// GetRPCs registers read-only Dex Web views.
func (flow *IdentityFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary returns the accepted outcome.
// dex:field attribute-key:posthog-identity-outcome value-type:json editable:false description:"Identity update outcome"
func (*IdentityFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readIdentityOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"posthog-identity-outcome": outcome}}, nil
}

// GetDexDisplay returns the accepted outcome.
// dex:field attribute-key:posthog-identity-outcome value-type:json editable:false description:"Identity update outcome"
func (*IdentityFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readIdentityOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"posthog-identity-outcome": outcome}}, nil
}

func readIdentityOutcome(ctx dex.Context) (posthog.CapturedEvent, error) {
	outcome, err := identityOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return posthog.CapturedEvent{}, nil
	}
	return outcome, err
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Fix the identity event time before calling PostHog."
type startIdentity struct{ dex.StepDefaults }

func (startIdentity) GetStepType() string { return "StartPostHogIdentifyPerson" }
func (startIdentity) WaitFor(dex.Context, posthog.IdentifyPersonInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startIdentity) Execute(_ dex.Context, input posthog.IdentifyPersonInput) (*dex.StepDecision, error) {
	if input.Timestamp == "" {
		input.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	}
	return dex.GoTo(sdkgo.StepRef[posthog.IdentifyPersonInput](identityStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete after PostHog accepts the identity event."
type acceptedIdentity struct {
	dex.StepDefaultsNoWaitFor[posthog.IdentifyPersonResult]
}

func (acceptedIdentity) GetStepType() string { return "PostHogIdentityAccepted" }
func (acceptedIdentity) Execute(ctx dex.Context, result posthog.IdentifyPersonResult) (*dex.StepDecision, error) {
	if err := identityOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*IdentityFlow)(nil)
