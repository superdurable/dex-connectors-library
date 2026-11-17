// Moveright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package folderdiscovery

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/dropbox/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// MoveFlowType is the stable file move example identity.
const MoveFlowType = "DropboxFileMove"

var movedEntryAttribute = dex.DefineAttribute[dropbox.EntryMetadata]("dropbox-moved-entry")

// MoveFlow runs one bounded Dropbox file move.
type MoveFlow struct {
	dex.FlowDefaults
	connection dropbox.Connection
}

// NewMoveFlow binds the provider connection at registration time.
func NewMoveFlow(connection dropbox.Connection) *MoveFlow {
	return &MoveFlow{connection: connection}
}

// GetFlowType preserves the file move Flow identity.
func (*MoveFlow) GetFlowType() string { return MoveFlowType }

// GetPersistenceSchema registers the move result Attribute.
func (*MoveFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{movedEntryAttribute}}
}

// GetSteps registers the operation-specific move Step.
func (flow *MoveFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startMove{}),
		dex.DefineStep(dropbox.NewMovePathStep(dropbox.MovePathStepConfig[dropbox.TransferPathInput]{
			StepType: "CallMovePath", ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Dropbox", Explanation: "Move one page of Dropbox files and folders."},
			Connection:          flow.connection,
			MapToOperationInput: mapMoveInput,
			Moved:               sdkgo.GoTo(completeMove{}),
		})),
		dex.DefineStep(completeMove{}),
	}
}

// GetRPCs registers Dex Web summary and display views.
func (flow *MoveFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:dropbox-moved-entry value-type:json editable:false description:"Move results"
// GetDexSummary shows the last move page in Dex Web.
func (*MoveFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readMovedEntry(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"dropbox-moved-entry": page}}, nil
}

// dex:field attribute-key:dropbox-moved-entry value-type:json editable:false description:"Move results"
// GetDexDisplay shows the last move page in Dex Web.
func (*MoveFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readMovedEntry(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"dropbox-moved-entry": page}}, nil
}

func readMovedEntry(ctx dex.Context) (dropbox.EntryMetadata, error) {
	page, err := movedEntryAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return dropbox.EntryMetadata{}, nil
	}
	return page, err
}

func mapMoveInput(input dropbox.TransferPathInput) dropbox.TransferPathInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the file move request to Dropbox."
type startMove struct{ dex.StepDefaults }

func (startMove) GetStepType() string { return "StartFileMove" }
func (startMove) WaitFor(dex.Context, dropbox.TransferPathInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startMove) Execute(_ dex.Context, input dropbox.TransferPathInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[dropbox.TransferPathInput]("CallMovePath"), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with one page of Dropbox move results."
type completeMove struct {
	dex.StepDefaultsNoWaitFor[dropbox.MovePathResult]
}

func (completeMove) GetStepType() string { return "CompleteFileMove" }
func (completeMove) Execute(ctx dex.Context, result dropbox.MovePathResult) (*dex.StepDecision, error) {
	if err := movedEntryAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*MoveFlow)(nil)
