// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package keydiscovery runs Upstash Redis getKey as one Dex Flow.
package keydiscovery

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/upstash/redis/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "UpstashRedisKeyLookup"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "upstash-redis"
	operationStepType = "CallGetKey"
)

var keyAttribute = dex.DefineAttribute[redis.KeyLookup]("redis-key-lookup")

// Flow runs GetKey once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection redis.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection redis.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{keyAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(redis.NewGetKeyStep(redis.GetKeyStepConfig[redis.GetKeyInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Upstash Redis", Explanation: "List one page of Upstash Redis keys."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Found:               sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:redis-key-lookup value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readKeyLookup(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"redis-key-lookup": outcome}}, nil
}

// dex:field attribute-key:redis-key-lookup value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readKeyLookup(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"redis-key-lookup": outcome}}, nil
}

func readKeyLookup(ctx dex.Context) (redis.KeyLookup, error) {
	outcome, err := keyAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return redis.KeyLookup{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input redis.GetKeyInput) redis.GetKeyInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetKey" }
func (startRequest) WaitFor(dex.Context, redis.GetKeyInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input redis.GetKeyInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[redis.GetKeyInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[redis.GetKeyResult]
}

func (completeResult) GetStepType() string { return "CompleteGetKey" }
func (completeResult) Execute(ctx dex.Context, result redis.GetKeyResult) (*dex.StepDecision, error) {
	if err := keyAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
