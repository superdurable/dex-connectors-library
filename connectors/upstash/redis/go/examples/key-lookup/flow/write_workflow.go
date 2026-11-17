// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package keydiscovery

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/upstash/redis/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// WriteFlowType is the stable key replacement example identity.
const WriteFlowType = "UpstashRedisKeyWrite"

var writtenKeyAttribute = dex.DefineAttribute[redis.KeyWrite]("redis-written-key")

// WriteFlow replaces one key with a complete string value.
type WriteFlow struct {
	dex.FlowDefaults
	connection redis.Connection
}

// NewWriteFlow binds the same static connection used by the read example.
func NewWriteFlow(connection redis.Connection) *WriteFlow { return &WriteFlow{connection: connection} }

func (*WriteFlow) GetFlowType() string { return WriteFlowType }

func (*WriteFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{writtenKeyAttribute}}
}

func (flow *WriteFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startWrite{}),
		dex.DefineStep(redis.NewSetKeyStep(redis.SetKeyStepConfig[redis.SetKeyInput]{
			StepType: "CallSetKey", ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Upstash Redis", Explanation: "Replace one Upstash Redis string value."},
			Connection:          flow.connection,
			MapToOperationInput: mapWriteInput,
			Set:                 sdkgo.GoTo(completeWrite{}),
		})),
		dex.DefineStep(completeWrite{}),
	}
}

// GetRPCs exposes the write result to Dex Web.
func (flow *WriteFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:redis-written-key value-type:json editable:false description:"Written key"
// GetDexSummary shows the last written key.
func (*WriteFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readWrittenKey(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"redis-written-key": value}}, nil
}

// dex:field attribute-key:redis-written-key value-type:json editable:false description:"Written key"
// GetDexDisplay shows the last written key.
func (*WriteFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readWrittenKey(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"redis-written-key": value}}, nil
}

func readWrittenKey(ctx dex.Context) (redis.KeyWrite, error) {
	value, err := writtenKeyAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return redis.KeyWrite{}, nil
	}
	return value, err
}

func mapWriteInput(input redis.SetKeyInput) redis.SetKeyInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed replacement request to the provider Step."
type startWrite struct{ dex.StepDefaults }

func (startWrite) GetStepType() string { return "StartSetKey" }
func (startWrite) WaitFor(dex.Context, redis.SetKeyInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startWrite) Execute(_ dex.Context, input redis.SetKeyInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[redis.SetKeyInput]("CallSetKey"), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the accepted Redis key."
type completeWrite struct {
	dex.StepDefaultsNoWaitFor[redis.SetKeyResult]
}

func (completeWrite) GetStepType() string { return "CompleteSetKey" }
func (completeWrite) Execute(ctx dex.Context, result redis.SetKeyResult) (*dex.StepDecision, error) {
	if err := writtenKeyAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*WriteFlow)(nil)
