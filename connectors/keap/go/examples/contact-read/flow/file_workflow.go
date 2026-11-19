// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contactread

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/keap/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// FileListFlowType identifies the file metadata example Flow.
const FileListFlowType = "KeapFileList"

const listFilesStepType = "CallListFiles"

var filesPageAttribute = dex.DefineAttribute[keap.FilesPage]("keap-files-page")

// FileListFlow lists one bounded page of files.
type FileListFlow struct {
	dex.FlowDefaults
	connection keap.Connection
}

// NewFileListFlow binds the Keap connection at registration time.
func NewFileListFlow(connection keap.Connection) *FileListFlow {
	return &FileListFlow{connection: connection}
}

func (*FileListFlow) GetFlowType() string { return FileListFlowType }

func (*FileListFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{filesPageAttribute}}
}

func (flow *FileListFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startFileList{}),
		dex.DefineStep(keap.NewListFilesStep(keap.ListFilesStepConfig[keap.ListFilesInput]{
			StepType: listFilesStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Keap", Explanation: "List a bounded page of CRM files."},
			Connection:  flow.connection, MapToOperationInput: MapToListFilesInput,
			Listed: sdkgo.GoTo(completeFileList{}),
		})),
		dex.DefineStep(completeFileList{}),
	}
}

func (flow *FileListFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:keap-files-page value-type:json editable:false description:"File page"
// GetDexSummary shows the file page in Dex Web.
func (*FileListFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readFilesPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"keap-files-page": page}}, nil
}

// dex:field attribute-key:keap-files-page value-type:json editable:false description:"File page"
// GetDexDisplay shows the file page in Dex Web.
func (*FileListFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readFilesPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"keap-files-page": page}}, nil
}

func readFilesPage(ctx dex.Context) (keap.FilesPage, error) {
	page, err := filesPageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return keap.FilesPage{}, nil
	}
	return page, err
}

// MapToListFilesInput passes the typed page request to Keap.
func MapToListFilesInput(input keap.ListFilesInput) keap.ListFilesInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the requested file page to the provider Step."
type startFileList struct{ dex.StepDefaults }

func (startFileList) GetStepType() string { return "StartFileList" }
func (startFileList) WaitFor(dex.Context, keap.ListFilesInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startFileList) Execute(_ dex.Context, input keap.ListFilesInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[keap.ListFilesInput](listFilesStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with one typed file page."
type completeFileList struct {
	dex.StepDefaultsNoWaitFor[keap.ListFilesResult]
}

func (completeFileList) GetStepType() string { return "CompleteFileList" }
func (completeFileList) Execute(ctx dex.Context, result keap.ListFilesResult) (*dex.StepDecision, error) {
	if err := filesPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*FileListFlow)(nil)
