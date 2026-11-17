// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package signaturerequestinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/dropbox/sign/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// ListFlowType is the stable signature request list example identity.
const ListFlowType = "DropboxSignSignatureRequestList"

var signatureRequestPageAttribute = dex.DefineAttribute[dropboxsign.SignatureRequestPage]("dropbox-sign-signature-request-page")

// ListFlow reads one bounded page of signature request statuses.
type ListFlow struct {
	dex.FlowDefaults
	connection dropboxsign.Connection
}

// NewListFlow binds the provider connection at registration time.
func NewListFlow(connection dropboxsign.Connection) *ListFlow {
	return &ListFlow{connection: connection}
}

// GetFlowType preserves the request list Flow identity.
func (*ListFlow) GetFlowType() string { return ListFlowType }

// GetPersistenceSchema registers the request page Attribute.
func (*ListFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{signatureRequestPageAttribute}}
}

// GetSteps registers the operation-specific list Step.
func (flow *ListFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startList{}),
		dex.DefineStep(dropboxsign.NewListSignatureRequestsStep(dropboxsign.ListSignatureRequestsStepConfig[dropboxsign.ListSignatureRequestsInput]{
			StepType: "CallListSignatureRequests", ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Dropbox Sign", Explanation: "Read one page of signature requests."},
			Connection:          flow.connection,
			MapToOperationInput: mapListInput,
			Listed:              sdkgo.GoTo(completeList{}),
		})),
		dex.DefineStep(completeList{}),
	}
}

// GetRPCs registers Dex Web summary and display views.
func (flow *ListFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:dropbox-sign-signature-request-page value-type:json editable:false description:"Signature request page"
// GetDexSummary shows the last request page in Dex Web.
func (*ListFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readRequestPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"dropbox-sign-signature-request-page": page}}, nil
}

// dex:field attribute-key:dropbox-sign-signature-request-page value-type:json editable:false description:"Signature request page"
// GetDexDisplay shows the last request page in Dex Web.
func (*ListFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readRequestPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"dropbox-sign-signature-request-page": page}}, nil
}

func readRequestPage(ctx dex.Context) (dropboxsign.SignatureRequestPage, error) {
	page, err := signatureRequestPageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return dropboxsign.SignatureRequestPage{}, nil
	}
	return page, err
}

func mapListInput(input dropboxsign.ListSignatureRequestsInput) dropboxsign.ListSignatureRequestsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the list request to Dropbox Sign."
type startList struct{ dex.StepDefaults }

func (startList) GetStepType() string { return "StartSignatureRequestList" }
func (startList) WaitFor(dex.Context, dropboxsign.ListSignatureRequestsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startList) Execute(_ dex.Context, input dropboxsign.ListSignatureRequestsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[dropboxsign.ListSignatureRequestsInput]("CallListSignatureRequests"), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with one page of signature request statuses."
type completeList struct {
	dex.StepDefaultsNoWaitFor[dropboxsign.ListSignatureRequestsResult]
}

func (completeList) GetStepType() string { return "CompleteSignatureRequestList" }
func (completeList) Execute(ctx dex.Context, result dropboxsign.ListSignatureRequestsResult) (*dex.StepDecision, error) {
	if err := signatureRequestPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*ListFlow)(nil)
