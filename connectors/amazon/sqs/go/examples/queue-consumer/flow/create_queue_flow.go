// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package queueconsumer

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/amazon/sqs/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// CreateQueueFlowType is the stable Dex Web Start Flow identity for creating one queue.
	CreateQueueFlowType = "AmazonSQSCreateQueue"
	createQueueStepType = "CallCreateQueue"
)

var createdQueueAttribute = dex.DefineAttribute[sqs.QueueIdentity]("amazon-sqs-created-queue")

// CreateQueueFlow creates one queue through the consumer example's SQS connection.
type CreateQueueFlow struct {
	dex.FlowDefaults
	connection sqs.Connection
}

// NewCreateQueueFlow binds the provider connection at registration time.
func NewCreateQueueFlow(connection sqs.Connection) *CreateQueueFlow {
	return &CreateQueueFlow{connection: connection}
}

// GetFlowType returns the stable queue creation Flow identity.
func (*CreateQueueFlow) GetFlowType() string { return CreateQueueFlowType }

// GetPersistenceSchema registers the created queue URL.
func (*CreateQueueFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{createdQueueAttribute}}
}

// GetRPCs exposes read-only Dex Web views of the queue URL.
func (flow *CreateQueueFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary returns the created queue URL when this Flow has one.
//
// dex:field attribute-key:amazon-sqs-created-queue value-type:json editable:false description:"Created SQS queue"
func (*CreateQueueFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	queue, err := readCreatedQueue(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"amazon-sqs-created-queue": queue}}, nil
}

// GetDexDisplay returns the created queue URL when this Flow has one.
//
// dex:field attribute-key:amazon-sqs-created-queue value-type:json editable:false description:"Created SQS queue"
func (*CreateQueueFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	queue, err := readCreatedQueue(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"amazon-sqs-created-queue": queue}}, nil
}

func readCreatedQueue(ctx dex.Context) (sqs.QueueIdentity, error) {
	queue, err := createdQueueAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return sqs.QueueIdentity{}, nil
	}
	return queue, err
}

// GetSteps registers the start, provider create, and completion Steps.
func (flow *CreateQueueFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startQueueCreation{}),
		dex.DefineStep(sqs.NewCreateQueueStep(sqs.CreateQueueStepConfig[sqs.CreateQueueInput]{
			StepType: createQueueStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Amazon SQS", Explanation: "Create a standard or FIFO SQS queue by name and absolute settings."},
			Connection:  flow.connection, MapToOperationInput: mapToCreateQueueInput,
			Created: sdkgo.GoTo(completeQueueCreation{}),
		})),
		dex.DefineStep(completeQueueCreation{}),
	}
}

func mapToCreateQueueInput(input sqs.CreateQueueInput) sqs.CreateQueueInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the queue name and settings to the SQS provider Step."
type startQueueCreation struct{ dex.StepDefaults }

func (startQueueCreation) GetStepType() string { return "StartCreateQueue" }
func (startQueueCreation) WaitFor(dex.Context, sqs.CreateQueueInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startQueueCreation) Execute(_ dex.Context, input sqs.CreateQueueInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[sqs.CreateQueueInput](createQueueStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with SQS's accepted queue URL."
type completeQueueCreation struct {
	dex.StepDefaultsNoWaitFor[sqs.CreateQueueResult]
}

func (completeQueueCreation) GetStepType() string { return "CompleteCreateQueue" }
func (completeQueueCreation) Execute(ctx dex.Context, result sqs.CreateQueueResult) (*dex.StepDecision, error) {
	if err := createdQueueAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*CreateQueueFlow)(nil)
