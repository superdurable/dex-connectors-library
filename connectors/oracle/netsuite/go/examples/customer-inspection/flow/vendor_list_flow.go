// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package customerinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/oracle/netsuite/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// VendorListFlowType is the stable vendor listing Flow identity.
const VendorListFlowType = "OracleNetSuiteVendorListing"

const vendorListOperationStepType = "CallListVendors"

var vendorListAttribute = dex.DefineAttribute[oraclenetsuite.VendorList]("oracle-netsuite-vendor-list")

// VendorListFlow lists one page of vendors.
type VendorListFlow struct {
	dex.FlowDefaults
	connection oraclenetsuite.Connection
}

// NewVendorListFlow binds the connection for a vendor listing.
func NewVendorListFlow(connection oraclenetsuite.Connection) *VendorListFlow {
	return &VendorListFlow{connection: connection}
}

func (*VendorListFlow) GetFlowType() string { return VendorListFlowType }

func (*VendorListFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{vendorListAttribute}}
}

func (flow *VendorListFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startVendorListRequest{}),
		dex.DefineStep(oraclenetsuite.NewListVendorsStep(oraclenetsuite.ListVendorsStepConfig[oraclenetsuite.ListVendorsInput]{
			StepType: vendorListOperationStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Oracle NetSuite", Explanation: "List one page of vendor records from NetSuite."},
			Connection:  flow.connection, MapToOperationInput: mapVendorListInput,
			Listed: sdkgo.GoTo(completeVendorListResult{}),
		})),
		dex.DefineStep(completeVendorListResult{}),
	}
}

func (flow *VendorListFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:oracle-netsuite-vendor-list value-type:json editable:false description:"Vendor page"
func (*VendorListFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readVendorList(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"oracle-netsuite-vendor-list": page}}, nil
}

// dex:field attribute-key:oracle-netsuite-vendor-list value-type:json editable:false description:"Vendor page"
func (*VendorListFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readVendorList(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"oracle-netsuite-vendor-list": page}}, nil
}

func readVendorList(ctx dex.Context) (oraclenetsuite.VendorList, error) {
	page, err := vendorListAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return oraclenetsuite.VendorList{}, nil
	}
	return page, err
}

func mapVendorListInput(input oraclenetsuite.ListVendorsInput) oraclenetsuite.ListVendorsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the requested page size and offset to NetSuite."
type startVendorListRequest struct{ dex.StepDefaults }

func (startVendorListRequest) GetStepType() string { return "StartListVendors" }
func (startVendorListRequest) WaitFor(dex.Context, oraclenetsuite.ListVendorsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startVendorListRequest) Execute(_ dex.Context, input oraclenetsuite.ListVendorsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[oraclenetsuite.ListVendorsInput](vendorListOperationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the vendor page."
type completeVendorListResult struct {
	dex.StepDefaultsNoWaitFor[oraclenetsuite.ListVendorsResult]
}

func (completeVendorListResult) GetStepType() string { return "CompleteListVendors" }
func (completeVendorListResult) Execute(ctx dex.Context, result oraclenetsuite.ListVendorsResult) (*dex.StepDecision, error) {
	if err := vendorListAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*VendorListFlow)(nil)
