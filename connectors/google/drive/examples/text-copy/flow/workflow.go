// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package textcopy demonstrates every Google Drive operation in one Flow
// started from Dex Web Start Flow: resolve a file by exact name, read its text,
// upload a text copy, and read the copy's metadata back.
package textcopy

import (
	"errors"
	"strings"

	"github.com/superdurable/dex-connectors-library/connectors/google/drive"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity used by Dex Web Start Flow.
	FlowType = "GoogleDriveTextCopy"
	// ConnectionName is the static Dex Web connection for Google Drive.
	ConnectionName = "google-drive-files"

	recordRequestStepType    = "RecordTextCopyRequest"
	findSourceStepType       = "FindSourceFile"
	selectSourceStepType     = "SelectSourceFile"
	reportNotFoundStepType   = "ReportSourceNotFound"
	readSourceStepType       = "ReadSourceText"
	prepareCopyStepType      = "PrepareTextCopy"
	uploadCopyStepType       = "UploadTextCopy"
	readBackCopyStepType     = "ReadBackTextCopy"
	completeCopyStepType     = "CompleteTextCopy"
	sourceCandidatesPageSize = 10
)

var (
	requestAttribute          = dex.DefineAttribute[Input]("google-drive-text-copy-request")
	outcomeAttribute          = dex.DefineAttribute[Outcome]("google-drive-text-copy-outcome")
	copyUploadResultAttribute = dex.DefineAttribute[drive.UploadFileResult]("google-drive-text-copy-upload-result")
)

// Input is the typed request entered in Dex Web Start Flow.
type Input struct {
	// SourceName is the exact Drive file name to resolve, such as Ops Policy.
	SourceName string `json:"sourceName"`
	// CopyName is the name of the uploaded text copy, such as Ops Policy.txt.
	CopyName string `json:"copyName"`
}

// FolderConfiguration is the folder a Step's folderPicker unit saves in Dex Web.
type FolderConfiguration struct {
	// FolderID is the selected Drive folder ID; blank means no folder was chosen.
	FolderID string `json:"folderId,omitempty"`
	// FolderName is the selected folder's display name.
	FolderName string `json:"folderName,omitempty"`
}

// Status is the business outcome of one text copy.
type Status string

const (
	// StatusCopied means the source text was uploaded and read back.
	StatusCopied Status = "copied"
	// StatusSourceNotFound means no non-trashed file has the requested name.
	StatusSourceNotFound Status = "sourceNotFound"
	// StatusSourceAmbiguous means more than one file has the requested name, so nothing was copied.
	StatusSourceAmbiguous Status = "sourceAmbiguous"
)

// Outcome is the durable result of the Flow and its completion output.
type Outcome struct {
	// Status is the business outcome.
	Status Status `json:"status"`
	// Source is the uniquely resolved source file.
	Source *drive.FileSummary `json:"source,omitempty"`
	// SourceCandidates lists the files that made the name ambiguous.
	SourceCandidates []drive.FileSummary `json:"sourceCandidates,omitempty"`
	// IsIncompleteSearch reports that Drive did not search every document.
	IsIncompleteSearch bool `json:"isIncompleteSearch,omitempty"`
	// TextMimeType is the MIME type of the text read from the source.
	TextMimeType string `json:"textMimeType,omitempty"`
	// TextByteCount is the UTF-8 byte length of the copied text.
	TextByteCount int64 `json:"textByteCount,omitempty"`
	// Copy is the uploaded copy's metadata, read back from Drive.
	Copy *drive.File `json:"copy,omitempty"`
	// IsCopyFromEarlierAttempt reports that a retried upload reused the file an earlier attempt created.
	IsCopyFromEarlierAttempt bool `json:"isCopyFromEarlierAttempt,omitempty"`
}

// CopyRequest is the application input of the upload Step.
type CopyRequest struct {
	// Name is the copy's file name.
	Name string `json:"name"`
	// MimeType is the copy's text MIME type.
	MimeType string `json:"mimeType"`
	// Text is the copied text.
	Text string `json:"text"`
}

// Flow copies the text of one Drive file, resolved by exact name, into a new Drive file.
type Flow struct {
	dex.FlowDefaults
	connection        drive.Connection
	sourceFolder      sdkgo.ConnectorLoadedConfiguration[FolderConfiguration]
	destinationFolder sdkgo.ConnectorLoadedConfiguration[FolderConfiguration]
}

// NewFlow binds the Google Drive Connection and the folder picks loaded at
// startup. A blank source folder searches every visible folder, and a blank
// destination folder uploads to the My Drive root.
func NewFlow(
	connection drive.Connection,
	sourceFolder sdkgo.ConnectorLoadedConfiguration[FolderConfiguration],
	destinationFolder sdkgo.ConnectorLoadedConfiguration[FolderConfiguration],
) *Flow {
	return &Flow{connection: connection, sourceFolder: sourceFolder, destinationFolder: destinationFolder}
}

// SourceFolderConfigurationRef identifies the source folder pick of the search Step.
func SourceFolderConfigurationRef() sdkgo.ConnectorConfigurationRef {
	return sdkgo.ConnectorConfigurationRef{
		ConnectorID: drive.ConnectorID, ConnectionName: ConnectionName, OperationID: "searchFiles",
		FlowType: FlowType, StepType: findSourceStepType,
	}
}

// DestinationFolderConfigurationRef identifies the destination folder pick of the upload Step.
func DestinationFolderConfigurationRef() sdkgo.ConnectorConfigurationRef {
	return sdkgo.ConnectorConfigurationRef{
		ConnectorID: drive.ConnectorID, ConnectionName: ConnectionName, OperationID: "uploadFile",
		FlowType: FlowType, StepType: uploadCopyStepType,
	}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the request, Google Drive, and outcome Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordCopyRequest{}),
		dex.DefineStep(drive.NewSearchFilesStep(drive.SearchFilesStepConfig[Input]{
			StepType: findSourceStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "google-drive", GroupLabel: "Google Drive",
				Explanation: "Resolve the source file name to Drive file IDs, excluding trashed files.",
			},
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{{
				ID: "sourceFolder", UnitID: drive.UIUnitFolderPicker, Label: "Source folder",
				Description: "Choose the folder whose direct children are searched for the source file name; leave blank to search every folder the connection can see.",
				Bindings: []sdkgo.ConnectorUIBinding{
					{Port: drive.UIFolderPickerPortFolderID, JSONPointer: "/folderId"},
					{Port: drive.UIFolderPickerPortFolderName, JSONPointer: "/folderName"},
				},
			}}},
			Connection: flow.connection, MapToOperationInput: flow.MapToSearchFilesInput,
			Found:    sdkgo.GoTo(selectSourceFile{}),
			NotFound: sdkgo.GoTo(reportSourceNotFound{}),
		})),
		dex.DefineStep(selectSourceFile{}),
		dex.DefineStep(reportSourceNotFound{}),
		dex.DefineStep(drive.NewReadFileTextStep(drive.ReadFileTextStepConfig[drive.FileSummary]{
			StepType: readSourceStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "google-drive", GroupLabel: "Google Drive",
				Explanation: "Export or download the resolved source file as bounded UTF-8 text.",
			},
			Connection: flow.connection, MapToOperationInput: MapToReadFileTextInput,
			Read: sdkgo.GoTo(prepareTextCopy{}),
		})),
		dex.DefineStep(prepareTextCopy{}),
		dex.DefineStep(drive.NewUploadFileStep(drive.UploadFileStepConfig[CopyRequest]{
			StepType: uploadCopyStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "google-drive", GroupLabel: "Google Drive",
				Explanation: "Upload the text copy once, reusing the file an earlier attempt created.",
			},
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{{
				ID: "destinationFolder", UnitID: drive.UIUnitFolderPicker, Label: "Destination folder",
				Description: "Choose the folder that receives the text copy; leave blank to create the copy in the My Drive root.",
				Bindings: []sdkgo.ConnectorUIBinding{
					{Port: drive.UIFolderPickerPortFolderID, JSONPointer: "/folderId"},
					{Port: drive.UIFolderPickerPortFolderName, JSONPointer: "/folderName"},
				},
			}}},
			Connection: flow.connection, MapToOperationInput: flow.MapToUploadFileInput,
			Uploaded:        sdkgo.GoTo(sdkgo.StepRef[drive.UploadFileResult](readBackCopyStepType)),
			ResultAttribute: &copyUploadResultAttribute,
		})),
		dex.DefineStep(drive.NewGetFileStep(drive.GetFileStepConfig[drive.UploadFileResult]{
			StepType: readBackCopyStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "google-drive", GroupLabel: "Google Drive",
				Explanation: "Read the uploaded copy's metadata back from Drive to confirm it exists.",
			},
			Connection: flow.connection, MapToOperationInput: MapToGetFileInput,
			Found: sdkgo.GoTo(completeTextCopy{}),
		})),
		dex.DefineStep(completeTextCopy{}),
	}
}

// GetRPCs returns the summary and display RPCs used by Dex Web.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetPersistenceSchema registers the request, outcome, and upload result Attributes.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{requestAttribute, outcomeAttribute, copyUploadResultAttribute}}
}

// GetDexSummary returns the request and outcome.
//
// dex:field attribute-key:google-drive-text-copy-request value-type:json editable:false description:"Requested source and copy names"
// dex:field attribute-key:google-drive-text-copy-outcome value-type:json editable:false description:"Text copy outcome"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, outcome, err := textCopyInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"google-drive-text-copy-request": request,
		"google-drive-text-copy-outcome": outcome,
	}}, nil
}

// GetDexDisplay returns the request and outcome.
//
// dex:field attribute-key:google-drive-text-copy-request value-type:json editable:false description:"Source file name and copy name"
// dex:field attribute-key:google-drive-text-copy-outcome value-type:json editable:false description:"Resolved source, candidates, and uploaded copy"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, outcome, err := textCopyInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"google-drive-text-copy-request": request,
		"google-drive-text-copy-outcome": outcome,
	}}, nil
}

// MapToSearchFilesInput resolves the exact source name, scoped by the saved source folder.
func (flow *Flow) MapToSearchFilesInput(input Input) drive.SearchFilesInput {
	return drive.SearchFilesInput{
		Name: input.SourceName, NameMatch: drive.NameMatchExact,
		ParentFolderID: flow.sourceFolder.Value.FolderID, PageSize: sourceCandidatesPageSize,
	}
}

// MapToReadFileTextInput reads the resolved source file.
func MapToReadFileTextInput(source drive.FileSummary) drive.ReadFileTextInput {
	return drive.ReadFileTextInput{FileID: source.ID}
}

// MapToUploadFileInput uploads the copy into the saved destination folder.
func (flow *Flow) MapToUploadFileInput(request CopyRequest) drive.UploadFileInput {
	return drive.UploadFileInput{
		Name: request.Name, ParentFolderID: flow.destinationFolder.Value.FolderID,
		MimeType: request.MimeType, TextContent: request.Text,
	}
}

// MapToGetFileInput reads back the uploaded copy.
func MapToGetFileInput(result drive.UploadFileResult) drive.GetFileInput {
	return drive.GetFileInput{FileID: result.Value.File.ID}
}

func textCopyInspection(ctx dex.Context) (Input, Outcome, error) {
	request, err := optionalAttribute(ctx, requestAttribute)
	if err != nil {
		return Input{}, Outcome{}, err
	}
	outcome, err := optionalAttribute(ctx, outcomeAttribute)
	if err != nil {
		return Input{}, Outcome{}, err
	}
	return request, outcome, nil
}

func optionalAttribute[T any](ctx dex.Context, attribute dex.Attribute[T]) (T, error) {
	value, err := attribute.Get(ctx)
	var missingAttribute *dex.AttributeNotFoundError
	if errors.As(err, &missingAttribute) {
		var zero T
		return zero, nil
	}
	return value, err
}

// dex:group group-id:text-copy group-label:"Text copy"
// dex:explanation text:"Validate and record the requested source and copy names."
type recordCopyRequest struct {
	dex.StepDefaults
}

func (recordCopyRequest) GetStepType() string { return recordRequestStepType }

// WaitFor skips immediately because Dex Web invokes the start Step's WaitFor.
func (recordCopyRequest) WaitFor(dex.Context, Input) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (recordCopyRequest) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	input.CopyName = strings.TrimSpace(input.CopyName)
	if strings.TrimSpace(input.SourceName) == "" || input.CopyName == "" {
		return dex.ForceFail("sourceName and copyName are required"), nil
	}
	if err := requestAttribute.Set(ctx, input); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[Input](findSourceStepType), input), nil
}

// dex:group group-id:text-copy group-label:"Text copy"
// dex:explanation text:"Continue only when exactly one file has the name; otherwise complete with the candidates."
type selectSourceFile struct {
	dex.StepDefaultsNoWaitFor[drive.SearchFilesResult]
}

func (selectSourceFile) GetStepType() string { return selectSourceStepType }

func (selectSourceFile) Execute(ctx dex.Context, result drive.SearchFilesResult) (*dex.StepDecision, error) {
	page := result.Value
	outcome := Outcome{IsIncompleteSearch: page.IsIncompleteSearch}
	if len(page.Files) != 1 || page.NextPageToken != "" || page.IsIncompleteSearch {
		outcome.Status = StatusSourceAmbiguous
		outcome.SourceCandidates = page.Files
		if err := outcomeAttribute.Set(ctx, outcome); err != nil {
			return nil, err
		}
		return dex.GracefulComplete(outcome), nil
	}
	source := page.Files[0]
	outcome.Source = &source
	if err := outcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[drive.FileSummary](readSourceStepType), source), nil
}

// dex:group group-id:text-copy group-label:"Text copy"
// dex:explanation text:"Complete with sourceNotFound when no non-trashed file has the name."
type reportSourceNotFound struct {
	dex.StepDefaultsNoWaitFor[drive.SearchFilesResult]
}

func (reportSourceNotFound) GetStepType() string { return reportNotFoundStepType }

func (reportSourceNotFound) Execute(ctx dex.Context, result drive.SearchFilesResult) (*dex.StepDecision, error) {
	outcome := Outcome{Status: StatusSourceNotFound, IsIncompleteSearch: result.Value.IsIncompleteSearch}
	if err := outcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

// dex:group group-id:text-copy group-label:"Text copy"
// dex:explanation text:"Record the read text's size and prepare the copy upload."
type prepareTextCopy struct {
	dex.StepDefaultsNoWaitFor[drive.ReadFileTextResult]
}

func (prepareTextCopy) GetStepType() string { return prepareCopyStepType }

func (prepareTextCopy) Execute(ctx dex.Context, result drive.ReadFileTextResult) (*dex.StepDecision, error) {
	request, err := requestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome, err := outcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome.TextMimeType = result.Value.TextMimeType
	outcome.TextByteCount = result.Value.ByteCount
	if err := outcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	copyRequest := CopyRequest{Name: request.CopyName, MimeType: result.Value.TextMimeType, Text: result.Value.Text}
	return dex.GoTo(sdkgo.StepRef[CopyRequest](uploadCopyStepType), copyRequest), nil
}

// dex:group group-id:text-copy group-label:"Text copy"
// dex:explanation text:"Record the read-back copy and complete the Flow."
type completeTextCopy struct {
	dex.StepDefaultsNoWaitFor[drive.GetFileResult]
}

func (completeTextCopy) GetStepType() string { return completeCopyStepType }

func (completeTextCopy) Execute(ctx dex.Context, result drive.GetFileResult) (*dex.StepDecision, error) {
	outcome, err := outcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	uploadResult, err := copyUploadResultAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	copiedFile := result.Value
	outcome.Status = StatusCopied
	outcome.Copy = &copiedFile
	outcome.IsCopyFromEarlierAttempt = uploadResult.Value.IsFromEarlierAttempt
	if err := outcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
