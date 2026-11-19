// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package templatelist

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/sendgrid/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// MailFlowType identifies the transactional mail example.
	MailFlowType = "SendGridTransactionalMailDemo"
	mailStepType = "CallSendMail"
)

var mailOutcomeAttribute = dex.DefineAttribute[sendgrid.SendMailOutput]("sendgrid-mail-accepted-outcome")

// MailFlow sends one synthetic transactional email request.
type MailFlow struct {
	dex.FlowDefaults
	connection sendgrid.Connection
}

// NewMailFlow binds the static SendGrid connection.
func NewMailFlow(connection sendgrid.Connection) *MailFlow { return &MailFlow{connection: connection} }

// GetFlowType returns the stable mail Flow identity.
func (*MailFlow) GetFlowType() string { return MailFlowType }

// GetPersistenceSchema registers the acceptance outcome.
func (*MailFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{mailOutcomeAttribute}}
}

// GetSteps registers request, send, and acceptance Steps.
func (flow *MailFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startMailRequest{}),
		dex.DefineStep(sendgrid.NewSendMailStep(sendgrid.SendMailStepConfig[sendgrid.SendMailInput]{
			StepType: mailStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "SendGrid", Explanation: "Send one transactional email with a bounded duplicate-send budget."},
			Connection:  flow.connection, MapToOperationInput: func(input sendgrid.SendMailInput) sendgrid.SendMailInput { return input },
			Accepted: sdkgo.GoTo(completeMailAcceptance{}),
		})),
		dex.DefineStep(completeMailAcceptance{}),
	}
}

// GetRPCs exposes Dex Web summary and display views.
func (flow *MailFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:sendgrid-mail-accepted-outcome value-type:json editable:false description:"Mail acceptance"
// GetDexSummary returns the saved acceptance result.
func (*MailFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	output, err := readMailOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"sendgrid-mail-accepted-outcome": output}}, nil
}

// dex:field attribute-key:sendgrid-mail-accepted-outcome value-type:json editable:false description:"Mail acceptance"
// GetDexDisplay returns the saved acceptance result.
func (*MailFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	output, err := readMailOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"sendgrid-mail-accepted-outcome": output}}, nil
}

func readMailOutcome(ctx dex.Context) (sendgrid.SendMailOutput, error) {
	output, err := mailOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return sendgrid.SendMailOutput{}, nil
	}
	return output, err
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed mail request to SendGrid."
type startMailRequest struct{ dex.StepDefaults }

func (startMailRequest) GetStepType() string { return "StartMailRequest" }
func (startMailRequest) WaitFor(dex.Context, sendgrid.SendMailInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startMailRequest) Execute(_ dex.Context, input sendgrid.SendMailInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[sendgrid.SendMailInput](mailStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Save SendGrid acceptance and complete the Flow."
type completeMailAcceptance struct {
	dex.StepDefaultsNoWaitFor[sendgrid.SendMailResult]
}

func (completeMailAcceptance) GetStepType() string { return "CompleteMailAcceptance" }
func (completeMailAcceptance) Execute(ctx dex.Context, result sendgrid.SendMailResult) (*dex.StepDecision, error) {
	if err := mailOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*MailFlow)(nil)
