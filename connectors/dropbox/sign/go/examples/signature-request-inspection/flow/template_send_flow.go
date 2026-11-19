// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package signaturerequestinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/dropbox/sign/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// TemplateSendFlowType is the stable template send example identity.
const TemplateSendFlowType = "DropboxSignTemplateSend"

var templateSendStatusAttribute = dex.DefineAttribute[dropboxsign.SignatureRequestStatus]("dropbox-sign-template-send-status")

// TemplateSendFlow sends a signature request from an accessible template.
type TemplateSendFlow struct {
	dex.FlowDefaults
	connection dropboxsign.Connection
}

// NewTemplateSendFlow binds the provider connection at registration time.
func NewTemplateSendFlow(connection dropboxsign.Connection) *TemplateSendFlow {
	return &TemplateSendFlow{connection: connection}
}

// GetFlowType preserves the template send Flow identity.
func (*TemplateSendFlow) GetFlowType() string { return TemplateSendFlowType }

// GetPersistenceSchema registers the sent request status Attribute.
func (*TemplateSendFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{templateSendStatusAttribute}}
}

// GetSteps registers the operation-specific template send Step.
func (flow *TemplateSendFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startTemplateSend{}),
		dex.DefineStep(dropboxsign.NewSendSignatureRequestWithTemplateStep(dropboxsign.SendSignatureRequestWithTemplateStepConfig[dropboxsign.SendSignatureRequestWithTemplateInput]{
			StepType: "CallSendSignatureRequestWithTemplate", ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Dropbox Sign", Explanation: "Send one templated signature request."},
			Connection:          flow.connection,
			MapToOperationInput: mapTemplateSendInput,
			Sent:                sdkgo.GoTo(completeTemplateSend{}),
		})),
		dex.DefineStep(completeTemplateSend{}),
	}
}

// GetRPCs registers Dex Web summary and display views.
func (flow *TemplateSendFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:dropbox-sign-template-send-status value-type:json editable:false description:"Sent request status"
// GetDexSummary shows the last sent request in Dex Web.
func (*TemplateSendFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	status, err := readTemplateSendStatus(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"dropbox-sign-template-send-status": status}}, nil
}

// dex:field attribute-key:dropbox-sign-template-send-status value-type:json editable:false description:"Sent request status"
// GetDexDisplay shows the last sent request in Dex Web.
func (*TemplateSendFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	status, err := readTemplateSendStatus(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"dropbox-sign-template-send-status": status}}, nil
}

func readTemplateSendStatus(ctx dex.Context) (dropboxsign.SignatureRequestStatus, error) {
	status, err := templateSendStatusAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return dropboxsign.SignatureRequestStatus{}, nil
	}
	return status, err
}

func mapTemplateSendInput(input dropboxsign.SendSignatureRequestWithTemplateInput) dropboxsign.SendSignatureRequestWithTemplateInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass a templated signature request to Dropbox Sign."
type startTemplateSend struct{ dex.StepDefaults }

func (startTemplateSend) GetStepType() string { return "StartTemplateSend" }
func (startTemplateSend) WaitFor(dex.Context, dropboxsign.SendSignatureRequestWithTemplateInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startTemplateSend) Execute(_ dex.Context, input dropboxsign.SendSignatureRequestWithTemplateInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[dropboxsign.SendSignatureRequestWithTemplateInput]("CallSendSignatureRequestWithTemplate"), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with the newly sent signature request status."
type completeTemplateSend struct {
	dex.StepDefaultsNoWaitFor[dropboxsign.SendSignatureRequestWithTemplateResult]
}

func (completeTemplateSend) GetStepType() string { return "CompleteTemplateSend" }
func (completeTemplateSend) Execute(ctx dex.Context, result dropboxsign.SendSignatureRequestWithTemplateResult) (*dex.StepDecision, error) {
	if err := templateSendStatusAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*TemplateSendFlow)(nil)
