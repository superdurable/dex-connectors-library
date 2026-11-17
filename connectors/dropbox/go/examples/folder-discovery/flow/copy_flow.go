// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package folderdiscovery

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/dropbox/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// CopyFlowType is the stable file copy example identity.
const CopyFlowType = "DropboxFileCopy"

var copiedEntryAttribute = dex.DefineAttribute[dropbox.EntryMetadata]("dropbox-copied-entry")

// CopyFlow runs one bounded Dropbox file copy.
type CopyFlow struct {
	dex.FlowDefaults
	connection dropbox.Connection
}

// NewCopyFlow binds the provider connection at registration time.
func NewCopyFlow(connection dropbox.Connection) *CopyFlow {
	return &CopyFlow{connection: connection}
}

// GetFlowType preserves the file copy Flow identity.
func (*CopyFlow) GetFlowType() string { return CopyFlowType }

// GetPersistenceSchema registers the copy result Attribute.
func (*CopyFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{copiedEntryAttribute}}
}

// GetSteps registers the operation-specific copy Step.
func (flow *CopyFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startCopy{}),
		dex.DefineStep(dropbox.NewCopyPathStep(dropbox.CopyPathStepConfig[dropbox.TransferPathInput]{
			StepType: "CallCopyPath", ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Dropbox", Explanation: "Copy one page of Dropbox files and folders."},
			Connection:          flow.connection,
			MapToOperationInput: mapCopyInput,
			Copied:              sdkgo.GoTo(completeCopy{}),
		})),
		dex.DefineStep(completeCopy{}),
	}
}

// GetRPCs registers Dex Web summary and display views.
func (flow *CopyFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:dropbox-copied-entry value-type:json editable:false description:"Copy results"
// GetDexSummary shows the last copy page in Dex Web.
func (*CopyFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readCopiedEntry(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"dropbox-copied-entry": page}}, nil
}

// dex:field attribute-key:dropbox-copied-entry value-type:json editable:false description:"Copy results"
// GetDexDisplay shows the last copy page in Dex Web.
func (*CopyFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readCopiedEntry(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"dropbox-copied-entry": page}}, nil
}

func readCopiedEntry(ctx dex.Context) (dropbox.EntryMetadata, error) {
	page, err := copiedEntryAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return dropbox.EntryMetadata{}, nil
	}
	return page, err
}

func mapCopyInput(input dropbox.TransferPathInput) dropbox.TransferPathInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the file copy request to Dropbox."
type startCopy struct{ dex.StepDefaults }

func (startCopy) GetStepType() string { return "StartFileCopy" }
func (startCopy) WaitFor(dex.Context, dropbox.TransferPathInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startCopy) Execute(_ dex.Context, input dropbox.TransferPathInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[dropbox.TransferPathInput]("CallCopyPath"), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with one page of Dropbox copy results."
type completeCopy struct {
	dex.StepDefaultsNoWaitFor[dropbox.CopyPathResult]
}

func (completeCopy) GetStepType() string { return "CompleteFileCopy" }
func (completeCopy) Execute(ctx dex.Context, result dropbox.CopyPathResult) (*dex.StepDecision, error) {
	if err := copiedEntryAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*CopyFlow)(nil)
