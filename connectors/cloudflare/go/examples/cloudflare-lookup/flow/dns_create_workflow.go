// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package cloudflarelookup

import (
	"github.com/superdurable/dex-connectors-library/connectors/cloudflare/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// CreateDNSRecordFlowType is the identity for the DNS creation example.
const CreateDNSRecordFlowType = "CloudflareCreateDNSRecord"

const createDNSRecordStepType = "CreateCloudflareDNSRecord"

// CreateDNSRecordFlow creates a DNS record from an explicit Start Flow request.
type CreateDNSRecordFlow struct {
	dex.FlowDefaults
	connection cloudflare.Connection
}

// NewCreateDNSRecordFlow binds the static Cloudflare connection.
func NewCreateDNSRecordFlow(connection cloudflare.Connection) *CreateDNSRecordFlow {
	return &CreateDNSRecordFlow{connection: connection}
}

// GetFlowType returns the stable example Flow identity.
func (*CreateDNSRecordFlow) GetFlowType() string { return CreateDNSRecordFlowType }

// GetPersistenceSchema declares that this example stores no Attributes or Streams.
func (*CreateDNSRecordFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{}
}

// GetRPCs registers the read-only Dex Web views.
func (flow *CreateDNSRecordFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary returns the example's read-only summary.
func (*CreateDNSRecordFlow) GetDexSummary(_ dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{}}, nil
}

// GetDexDisplay returns the example's read-only display.
func (*CreateDNSRecordFlow) GetDexDisplay(_ dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{}}, nil
}

// GetSteps registers the input, provider, and completion Steps.
func (flow *CreateDNSRecordFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startDNSRecordCreation{}),
		dex.DefineStep(cloudflare.NewCreateDNSRecordStep(cloudflare.CreateDNSRecordStepConfig[cloudflare.CreateDNSRecordInput]{
			StepType: createDNSRecordStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "cloudflare-dns-create", GroupLabel: "Create DNS record", Explanation: "Create the requested DNS record in the selected zone."},
			MapToOperationInput: func(input cloudflare.CreateDNSRecordInput) cloudflare.CreateDNSRecordInput { return input },
			Created:             sdkgo.GoTo(completeDNSRecordCreation{}),
		})),
		dex.DefineStep(completeDNSRecordCreation{}),
	}
}

// dex:group group-id:cloudflare-dns-create group-label:"Create DNS record"
// dex:explanation text:"Start DNS record creation from the typed request."
type startDNSRecordCreation struct{ dex.StepDefaults }

func (startDNSRecordCreation) GetStepType() string { return "StartCloudflareCreateDNSRecord" }
func (startDNSRecordCreation) WaitFor(dex.Context, cloudflare.CreateDNSRecordInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startDNSRecordCreation) Execute(_ dex.Context, input cloudflare.CreateDNSRecordInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[cloudflare.CreateDNSRecordInput](createDNSRecordStepType), input), nil
}

// dex:group group-id:cloudflare-dns-create group-label:"Create DNS record"
// dex:explanation text:"Complete after Cloudflare returns the created DNS record."
type completeDNSRecordCreation struct {
	dex.StepDefaultsNoWaitFor[cloudflare.CreateDNSRecordResult]
}

func (completeDNSRecordCreation) GetStepType() string { return "CompleteCloudflareCreateDNSRecord" }
func (completeDNSRecordCreation) Execute(_ dex.Context, result cloudflare.CreateDNSRecordResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*CreateDNSRecordFlow)(nil)
