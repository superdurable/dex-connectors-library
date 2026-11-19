// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package agreementinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/adobe/acrobat-sign/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const listAgreementsStepType = "CallListAgreements"

var agreementsPageAttribute = dex.DefineAttribute[acrobatsign.AgreementPage]("adobe-acrobat-sign-agreements-page")

// ListAgreementsFlow demonstrates cursor paging without participant or document content.
type ListAgreementsFlow struct {
	dex.FlowDefaults
	connection acrobatsign.Connection
}

// NewListAgreementsFlow binds the same static Acrobat Sign connection.
func NewListAgreementsFlow(connection acrobatsign.Connection) *ListAgreementsFlow {
	return &ListAgreementsFlow{connection: connection}
}

// GetFlowType is the stable list example identity.
func (*ListAgreementsFlow) GetFlowType() string { return "AdobeAcrobatSignAgreementList" }

// GetPersistenceSchema registers the page shown by Dex Web.
func (*ListAgreementsFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{agreementsPageAttribute}}
}

// GetSteps registers one read boundary and a typed completion.
func (flow *ListAgreementsFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startAgreementList{}),
		dex.DefineStep(acrobatsign.NewListAgreementsStep(acrobatsign.ListAgreementsStepConfig[acrobatsign.ListAgreementsInput]{
			StepType: listAgreementsStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Adobe Acrobat Sign", Explanation: "Read one agreement list page."},
			Connection:  flow.connection, MapToOperationInput: MapToListAgreementsInput,
			Listed: sdkgo.GoTo(completeAgreementList{}),
		})),
		dex.DefineStep(completeAgreementList{}),
	}
}

// GetRPCs exposes the stored page in Dex Web.
func (flow *ListAgreementsFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:adobe-acrobat-sign-agreements-page value-type:json editable:false description:"Agreement page"
// GetDexSummary shows the agreement page in Dex Web.
func (*ListAgreementsFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readAgreementsPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"adobe-acrobat-sign-agreements-page": page}}, nil
}

// dex:field attribute-key:adobe-acrobat-sign-agreements-page value-type:json editable:false description:"Agreement page"
// GetDexDisplay shows the agreement page in Dex Web.
func (*ListAgreementsFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readAgreementsPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"adobe-acrobat-sign-agreements-page": page}}, nil
}

func readAgreementsPage(ctx dex.Context) (acrobatsign.AgreementPage, error) {
	page, err := agreementsPageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return acrobatsign.AgreementPage{}, nil
	}
	return page, err
}

// MapToListAgreementsInput preserves the typed paging input.
func MapToListAgreementsInput(input acrobatsign.ListAgreementsInput) acrobatsign.ListAgreementsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the cursor and page size to the provider Step."
type startAgreementList struct{ dex.StepDefaults }

func (startAgreementList) GetStepType() string { return "StartListAgreements" }
func (startAgreementList) WaitFor(dex.Context, acrobatsign.ListAgreementsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startAgreementList) Execute(_ dex.Context, input acrobatsign.ListAgreementsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[acrobatsign.ListAgreementsInput](listAgreementsStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the selected agreement page."
type completeAgreementList struct {
	dex.StepDefaultsNoWaitFor[acrobatsign.ListAgreementsResult]
}

func (completeAgreementList) GetStepType() string { return "CompleteListAgreements" }
func (completeAgreementList) Execute(ctx dex.Context, result acrobatsign.ListAgreementsResult) (*dex.StepDecision, error) {
	if err := agreementsPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*ListAgreementsFlow)(nil)
