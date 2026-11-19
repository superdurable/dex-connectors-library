// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contactread

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/keap/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// EmailListFlowType is the email emailList example Flow identity.
const EmailListFlowType = "KeapEmailList"

const listEmailsStepType = "CallListEmails"

var emailsPageAttribute = dex.DefineAttribute[keap.EmailsPage]("keap-emails-page")

// EmailListFlow lists one bounded page of emails.
type EmailListFlow struct {
	dex.FlowDefaults
	connection keap.Connection
}

// NewEmailListFlow binds the Keap connection at registration time.
func NewEmailListFlow(connection keap.Connection) *EmailListFlow {
	return &EmailListFlow{connection: connection}
}

func (*EmailListFlow) GetFlowType() string { return EmailListFlowType }

func (*EmailListFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{emailsPageAttribute}}
}

func (flow *EmailListFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startEmailList{}),
		dex.DefineStep(keap.NewListEmailsStep(keap.ListEmailsStepConfig[keap.ListEmailsInput]{
			StepType: listEmailsStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Keap", Explanation: "List a bounded page of CRM emails."},
			Connection:  flow.connection, MapToOperationInput: MapToListEmailsInput,
			Listed: sdkgo.GoTo(completeEmailList{}),
		})),
		dex.DefineStep(completeEmailList{}),
	}
}

func (flow *EmailListFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:keap-emails-page value-type:json editable:false description:"Email page"
// GetDexSummary shows the email page in Dex Web.
func (*EmailListFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readEmailsPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"keap-emails-page": page}}, nil
}

// dex:field attribute-key:keap-emails-page value-type:json editable:false description:"Email page"
// GetDexDisplay shows the email page in Dex Web.
func (*EmailListFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readEmailsPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"keap-emails-page": page}}, nil
}

func readEmailsPage(ctx dex.Context) (keap.EmailsPage, error) {
	page, err := emailsPageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return keap.EmailsPage{}, nil
	}
	return page, err
}

// MapToListEmailsInput passes the typed page request to Keap.
func MapToListEmailsInput(input keap.ListEmailsInput) keap.ListEmailsInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the requested email page to the provider Step."
type startEmailList struct{ dex.StepDefaults }

func (startEmailList) GetStepType() string { return "StartEmailList" }
func (startEmailList) WaitFor(dex.Context, keap.ListEmailsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startEmailList) Execute(_ dex.Context, input keap.ListEmailsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[keap.ListEmailsInput](listEmailsStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with one typed email page."
type completeEmailList struct {
	dex.StepDefaultsNoWaitFor[keap.ListEmailsResult]
}

func (completeEmailList) GetStepType() string { return "CompleteEmailList" }
func (completeEmailList) Execute(ctx dex.Context, result keap.ListEmailsResult) (*dex.StepDecision, error) {
	if err := emailsPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*EmailListFlow)(nil)
