// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package blobdownload

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/microsoft/azure-blob/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const listContainerStepType = "CallListContainers"

var containerListingAttribute = dex.DefineAttribute[azureblob.ContainersPage]("azureblob-containers-page")

// ContainerListingFlow lists one page of container names in the account.
type ContainerListingFlow struct {
	dex.FlowDefaults
	connection azureblob.Connection
}

// NewContainerListingFlow binds the existing named Azure connection.
func NewContainerListingFlow(connection azureblob.Connection) *ContainerListingFlow {
	return &ContainerListingFlow{connection: connection}
}

// GetPersistenceSchema registers the page stored by this Flow.
func (*ContainerListingFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{containerListingAttribute}}
}

// GetSteps registers the request, provider, and completion Steps.
func (flow *ContainerListingFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startContainerListing{}),
		dex.DefineStep(azureblob.NewListContainersStep(azureblob.ListContainersStepConfig[azureblob.ListContainersInput]{
			StepType: listContainerStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Azure Blob Storage", Explanation: "List one page of container names."},
			Connection:          flow.connection,
			MapToOperationInput: func(input azureblob.ListContainersInput) azureblob.ListContainersInput { return input },
			Listed:              sdkgo.GoTo(completeContainerListing{}),
		})),
		dex.DefineStep(completeContainerListing{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *ContainerListingFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:azureblob-containers-page value-type:json editable:false description:"Container listing"
// GetDexSummary shows the listed names in Dex Web.
func (*ContainerListingFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readContainerListing(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"azureblob-containers-page": page}}, nil
}

// dex:field attribute-key:azureblob-containers-page value-type:json editable:false description:"Container listing"
// GetDexDisplay shows the listed names in Dex Web.
func (*ContainerListingFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readContainerListing(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"azureblob-containers-page": page}}, nil
}

func readContainerListing(ctx dex.Context) (azureblob.ContainersPage, error) {
	page, err := containerListingAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return azureblob.ContainersPage{}, nil
	}
	return page, err
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the prefix and cursor to the provider Step."
type startContainerListing struct{ dex.StepDefaults }

func (startContainerListing) WaitFor(dex.Context, azureblob.ListContainersInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startContainerListing) Execute(_ dex.Context, input azureblob.ListContainersInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[azureblob.ListContainersInput](listContainerStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Store the container listing and complete the Flow."
type completeContainerListing struct {
	dex.StepDefaultsNoWaitFor[azureblob.ListContainersResult]
}

func (completeContainerListing) Execute(ctx dex.Context, result azureblob.ListContainersResult) (*dex.StepDecision, error) {
	if err := containerListingAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*ContainerListingFlow)(nil)
