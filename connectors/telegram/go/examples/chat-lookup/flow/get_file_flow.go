// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package chatlookup

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/telegram/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const getFileStepType = "CallGetFile"

var inspectedFileAttribute = dex.DefineAttribute[telegram.TelegramFile]("telegram-inspected-file")

// GetFileFlow reads Telegram file metadata through the static connection.
type GetFileFlow struct {
	dex.FlowDefaults
	connection telegram.Connection
}

// NewGetFileFlow binds the connection at Worker registration.
func NewGetFileFlow(connection telegram.Connection) *GetFileFlow {
	return &GetFileFlow{connection: connection}
}

// GetFlowType returns the stable file inspection Flow identity.
func (*GetFileFlow) GetFlowType() string { return "TelegramGetFile" }

// GetPersistenceSchema registers file metadata.
func (*GetFileFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{inspectedFileAttribute}}
}

// GetSteps registers the file query and completion Steps.
func (flow *GetFileFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(beginGetFile{}),
		dex.DefineStep(telegram.NewGetFileStep(telegram.GetFileStepConfig[telegram.GetFileInput]{
			StepType: getFileStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Telegram", Explanation: "Read file metadata without a token-bearing URL."},
			MapToOperationInput: mapGetFileInput, Found: sdkgo.GoTo(completeGetFile{}),
		})),
		dex.DefineStep(completeGetFile{}),
	}
}

// GetRPCs exposes read-only file metadata in Dex Web.
func (flow *GetFileFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary returns file metadata when available.
// dex:field attribute-key:telegram-inspected-file value-type:json editable:false description:"File metadata"
func (*GetFileFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	file, err := readInspectedFile(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"telegram-inspected-file": file}}, nil
}

// GetDexDisplay returns file metadata when available.
// dex:field attribute-key:telegram-inspected-file value-type:json editable:false description:"File metadata"
func (*GetFileFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	file, err := readInspectedFile(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"telegram-inspected-file": file}}, nil
}

func readInspectedFile(ctx dex.Context) (telegram.TelegramFile, error) {
	file, err := inspectedFileAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return telegram.TelegramFile{}, nil
	}
	return file, err
}

func mapGetFileInput(input telegram.GetFileInput) telegram.GetFileInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the file ID to the provider Step."
type beginGetFile struct{ dex.StepDefaults }

func (beginGetFile) WaitFor(dex.Context, telegram.GetFileInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (beginGetFile) Execute(_ dex.Context, input telegram.GetFileInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[telegram.GetFileInput](getFileStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the file metadata."
type completeGetFile struct {
	dex.StepDefaultsNoWaitFor[telegram.GetFileResult]
}

func (completeGetFile) Execute(ctx dex.Context, result telegram.GetFileResult) (*dex.StepDecision, error) {
	if err := inspectedFileAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*GetFileFlow)(nil)
