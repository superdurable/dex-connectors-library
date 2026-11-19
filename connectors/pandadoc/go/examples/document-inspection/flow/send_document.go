// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package documentinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/pandadoc/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// SendDocumentFlowType identifies the document-send example.
const SendDocumentFlowType = "PandaDocSendDocument"

const sendDocumentStepType = "CallSendDocument"

var sentDocumentAttribute = dex.DefineAttribute[pandadoc.DocumentAction]("pandadoc-sent-document")

// SendDocumentFlow sends a draft through a bounded Connector Mutation.
type SendDocumentFlow struct {
	dex.FlowDefaults
	connection pandadoc.Connection
}

// NewSendDocumentFlow binds the PandaDoc connection at registration.
func NewSendDocumentFlow(connection pandadoc.Connection) *SendDocumentFlow {
	return &SendDocumentFlow{connection: connection}
}

// GetFlowType preserves this Flow's durable identity.
func (*SendDocumentFlow) GetFlowType() string { return SendDocumentFlowType }

// GetSteps registers the send action and result transition.
func (flow *SendDocumentFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startSendDocument{}),
		dex.DefineStep(pandadoc.NewSendDocumentStep(pandadoc.SendDocumentStepConfig[pandadoc.SendDocumentInput]{
			StepType: sendDocumentStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "pandadoc-send", GroupLabel: "PandaDoc send", Explanation: "Send the selected draft document."},
			MapToOperationInput: func(input pandadoc.SendDocumentInput) pandadoc.SendDocumentInput { return input },
			Sent:                sdkgo.GoTo(completeSendDocument{}),
		})),
		dex.DefineStep(completeSendDocument{}),
	}
}

// GetRPCs exposes the result in Dex Web.
func (flow *SendDocumentFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetPersistenceSchema registers the sent document Attribute.
func (*SendDocumentFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{sentDocumentAttribute}}
}

// GetDexSummary returns the accepted document ID.
// dex:field attribute-key:pandadoc-sent-document value-type:json editable:false description:"Sent document"
func (*SendDocumentFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readSentDocument(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"pandadoc-sent-document": outcome}}, nil
}

// GetDexDisplay returns the accepted document ID.
// dex:field attribute-key:pandadoc-sent-document value-type:json editable:false description:"Sent document"
func (*SendDocumentFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readSentDocument(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"pandadoc-sent-document": outcome}}, nil
}

func readSentDocument(ctx dex.Context) (pandadoc.DocumentAction, error) {
	outcome, err := sentDocumentAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return pandadoc.DocumentAction{}, nil
	}
	return outcome, err
}

// dex:group group-id:pandadoc-send-start group-label:"Prepare send"
// dex:explanation text:"Pass the document send input to the Connector Step."
type startSendDocument struct{ dex.StepDefaults }

func (startSendDocument) GetStepType() string { return "StartSendDocument" }
func (startSendDocument) WaitFor(dex.Context, pandadoc.SendDocumentInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startSendDocument) Execute(_ dex.Context, input pandadoc.SendDocumentInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[pandadoc.SendDocumentInput](sendDocumentStepType), input), nil
}

// dex:group group-id:pandadoc-send-complete group-label:"Complete send"
// dex:explanation text:"Store the accepted PandaDoc document ID."
type completeSendDocument struct {
	dex.StepDefaultsNoWaitFor[pandadoc.SendDocumentResult]
}

func (completeSendDocument) GetStepType() string { return "CompleteSendDocument" }
func (completeSendDocument) Execute(ctx dex.Context, result pandadoc.SendDocumentResult) (*dex.StepDecision, error) {
	if err := sentDocumentAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*SendDocumentFlow)(nil)
