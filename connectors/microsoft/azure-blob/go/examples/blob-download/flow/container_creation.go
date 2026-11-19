// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package blobdownload

import (
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/azure-blob/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const createContainerStepType = "CallCreateContainer"

// ContainerCreationFlow creates one Azure container and returns its identity.
type ContainerCreationFlow struct {
	dex.FlowDefaults
	connection azureblob.Connection
}

// NewContainerCreationFlow binds the named Azure connection to the create operation.
func NewContainerCreationFlow(connection azureblob.Connection) *ContainerCreationFlow {
	return &ContainerCreationFlow{connection: connection}
}

// GetPersistenceSchema declares that the creation Flow stores no Attributes.
func (*ContainerCreationFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{}
}

// GetSteps registers the start, provider, and completion Steps.
func (flow *ContainerCreationFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startContainerCreation{}),
		dex.DefineStep(azureblob.NewCreateContainerStep(azureblob.CreateContainerStepConfig[azureblob.CreateContainerInput]{
			StepType: createContainerStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Azure Blob Storage", Explanation: "Create one Azure Storage container."},
			Connection:          flow.connection,
			MapToOperationInput: func(input azureblob.CreateContainerInput) azureblob.CreateContainerInput { return input },
			Created:             sdkgo.GoTo(completeContainerCreation{}),
		})),
		dex.DefineStep(completeContainerCreation{}),
	}
}

// GetRPCs exposes Dex Web summary and display views for the creation Flow.
func (flow *ContainerCreationFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary returns the creation Flow's empty persistent view.
func (*ContainerCreationFlow) GetDexSummary(dex.Context, dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{}}, nil
}

// GetDexDisplay returns the creation Flow's empty persistent view.
func (*ContainerCreationFlow) GetDexDisplay(dex.Context, dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{}}, nil
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the container name to the provider Step."
type startContainerCreation struct{ dex.StepDefaults }

func (startContainerCreation) WaitFor(dex.Context, azureblob.CreateContainerInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (startContainerCreation) Execute(_ dex.Context, input azureblob.CreateContainerInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[azureblob.CreateContainerInput](createContainerStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the created container identity."
type completeContainerCreation struct {
	dex.StepDefaultsNoWaitFor[azureblob.CreateContainerResult]
}

func (completeContainerCreation) Execute(_ dex.Context, result azureblob.CreateContainerResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*ContainerCreationFlow)(nil)
