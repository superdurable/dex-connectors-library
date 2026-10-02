// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package textcopy demonstrates every OneDrive and SharePoint operation in one
// Flow started from Dex Web Start Flow: resolve a text file by exact name in a
// source folder, ensure a copy folder in a destination folder, read the text,
// upload the copy with an explicit conflict behavior, and read the copy back.
package textcopy

import (
	"errors"
	"mime"
	"strings"

	"github.com/superdurable/dex-connectors-library/connectors/microsoft/onedrive"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity used by Dex Web Start Flow.
	FlowType = "OneDriveTextCopy"
	// ConnectionName is the static Dex Web connection for OneDrive and SharePoint.
	ConnectionName = "onedrive-files"

	recordRequestStepType    = "RecordTextCopyRequest"
	findSourceStepType       = "FindSourceFile"
	selectSourceStepType     = "SelectSourceFile"
	reportNotFoundStepType   = "ReportSourceNotFound"
	ensureCopyFolderStepType = "EnsureCopyFolder"
	recordCopyFolderStepType = "RecordCopyFolder"
	readSourceStepType       = "ReadSourceText"
	prepareCopyStepType      = "PrepareTextCopy"
	uploadCopyStepType       = "UploadTextCopy"
	reportCopyExistsStepType = "ReportCopyAlreadyExists"
	readBackCopyStepType     = "ReadBackTextCopy"
	completeCopyStepType     = "CompleteTextCopy"
	fallbackTextMimeType     = "text/plain"
)

var (
	requestAttribute          = dex.DefineAttribute[Input]("onedrive-text-copy-request")
	outcomeAttribute          = dex.DefineAttribute[Outcome]("onedrive-text-copy-outcome")
	copyUploadResultAttribute = dex.DefineAttribute[onedrive.UploadFileResult]("onedrive-text-copy-upload-result")
)

// Input is the typed request entered in Dex Web Start Flow.
type Input struct {
	// SourceName is the exact name of the text file in the source folder, such as Ops Policy.txt.
	SourceName string `json:"sourceName"`
	// CopyFolderName is the folder ensured in the destination folder, such as Dex copies.
	CopyFolderName string `json:"copyFolderName"`
	// CopyName is the name of the uploaded copy, such as Ops Policy (copy).txt.
	CopyName string `json:"copyName"`
	// ShouldReplaceExisting replaces a different existing copy; false completes as copyAlreadyExists instead.
	ShouldReplaceExisting bool `json:"shouldReplaceExisting,omitempty"`
}

// LocationConfiguration is the drive and folder a Step's site, drive, and folder pickers save in Dex Web.
type LocationConfiguration struct {
	// SiteID is the SharePoint site the drive picker listed libraries from; the Flow does not use it.
	SiteID string `json:"siteId,omitempty"`
	// SiteName is the selected site's display name.
	SiteName string `json:"siteName,omitempty"`
	// DriveID is the selected drive; blank means the signed-in user's OneDrive.
	DriveID string `json:"driveId,omitempty"`
	// DriveName is the selected drive's display name.
	DriveName string `json:"driveName,omitempty"`
	// FolderID is the selected folder's item ID; blank means the drive root.
	FolderID string `json:"folderId,omitempty"`
	// FolderName is the selected folder's display name.
	FolderName string `json:"folderName,omitempty"`
}

// Status is the business outcome of one text copy.
type Status string

const (
	// StatusCopied means the copy was written, or already had exactly this text, and was read back.
	StatusCopied Status = "copied"
	// StatusSourceNotFound means the source folder has no item with the requested name.
	StatusSourceNotFound Status = "sourceNotFound"
	// StatusCopyAlreadyExists means a different item already has the copy's name and replacement was not requested.
	StatusCopyAlreadyExists Status = "copyAlreadyExists"
)

// Outcome is the durable result of the Flow and its completion output.
type Outcome struct {
	// Status is the business outcome.
	Status Status `json:"status"`
	// Source is the resolved source file.
	Source *onedrive.FileSummary `json:"source,omitempty"`
	// CopyFolder is the ensured copy folder.
	CopyFolder *onedrive.DriveItem `json:"copyFolder,omitempty"`
	// IsCopyFolderExisting reports that the copy folder already existed, possibly from an earlier attempt.
	IsCopyFolderExisting bool `json:"isCopyFolderExisting,omitempty"`
	// TextByteCount is the UTF-8 byte length of the copied text.
	TextByteCount int64 `json:"textByteCount,omitempty"`
	// Copy is the uploaded copy's metadata, read back from Microsoft Graph.
	Copy *onedrive.DriveItem `json:"copy,omitempty"`
	// IsCopyExistingIdentical reports that a file with exactly this text was already at the copy's path.
	IsCopyExistingIdentical bool `json:"isCopyExistingIdentical,omitempty"`
	// ExistingCopy is the item that blocked the copy for copyAlreadyExists.
	ExistingCopy *onedrive.DriveItem `json:"existingCopy,omitempty"`
}

// CopyFolderRequest is the application input of the copy-folder Step.
type CopyFolderRequest struct {
	// Name is the copy folder's name.
	Name string `json:"name"`
}

// CopyRequest is the application input of the upload Step.
type CopyRequest struct {
	// FolderID is the ensured copy folder's item ID.
	FolderID string `json:"folderId"`
	// Name is the copy's file name.
	Name string `json:"name"`
	// MimeType is the copy's media type.
	MimeType string `json:"mimeType"`
	// Text is the copied text.
	Text string `json:"text"`
	// ConflictBehavior is fail, or replace when the request asked to replace an existing copy.
	ConflictBehavior onedrive.ConflictBehavior `json:"conflictBehavior"`
}

// Flow copies one text file, resolved by exact name, into an ensured copy folder.
type Flow struct {
	dex.FlowDefaults
	connection          onedrive.Connection
	sourceLocation      sdkgo.ConnectorLoadedConfiguration[LocationConfiguration]
	destinationLocation sdkgo.ConnectorLoadedConfiguration[LocationConfiguration]
}

// NewFlow binds the OneDrive Connection and the location picks loaded at
// startup. A blank drive is the signed-in user's OneDrive and a blank folder
// is the drive root, for both the source and the destination.
func NewFlow(
	connection onedrive.Connection,
	sourceLocation sdkgo.ConnectorLoadedConfiguration[LocationConfiguration],
	destinationLocation sdkgo.ConnectorLoadedConfiguration[LocationConfiguration],
) *Flow {
	return &Flow{connection: connection, sourceLocation: sourceLocation, destinationLocation: destinationLocation}
}

// SourceLocationConfigurationRef identifies the source location picks of the search Step.
func SourceLocationConfigurationRef() sdkgo.ConnectorConfigurationRef {
	return sdkgo.ConnectorConfigurationRef{
		ConnectorID: onedrive.ConnectorID, ConnectionName: ConnectionName, OperationID: "searchFiles",
		FlowType: FlowType, StepType: findSourceStepType,
	}
}

// DestinationLocationConfigurationRef identifies the destination location picks of the copy-folder Step.
func DestinationLocationConfigurationRef() sdkgo.ConnectorConfigurationRef {
	return sdkgo.ConnectorConfigurationRef{
		ConnectorID: onedrive.ConnectorID, ConnectionName: ConnectionName, OperationID: "createFolder",
		FlowType: FlowType, StepType: ensureCopyFolderStepType,
	}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the request, OneDrive, and outcome Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordCopyRequest{}),
		dex.DefineStep(onedrive.NewSearchFilesStep(onedrive.SearchFilesStepConfig[Input]{
			StepType: findSourceStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "onedrive", GroupLabel: "OneDrive and SharePoint",
				Explanation: "Look up the source name in the source folder by path, which is exact and strongly consistent.",
			},
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{
				{
					ID: "sourceSite", UnitID: onedrive.UIUnitSitePicker, Label: "Source SharePoint site",
					Description: "Choose the SharePoint site whose document library holds the source file, so the source drive picker can list its libraries; leave blank when the source is in your own OneDrive.",
					Bindings: []sdkgo.ConnectorUIBinding{
						{Port: onedrive.UISitePickerPortSiteID, JSONPointer: "/siteId"}, {Port: onedrive.UISitePickerPortSiteName, JSONPointer: "/siteName"},
					},
				},
				{
					ID: "sourceDrive", UnitID: onedrive.UIUnitDrivePicker, Label: "Source drive",
					Description: "Choose your OneDrive or a document library of the source site; leave blank to read from your own OneDrive.",
					Bindings: []sdkgo.ConnectorUIBinding{
						{Port: onedrive.UIDrivePickerPortSiteID, JSONPointer: "/siteId"},
						{Port: onedrive.UIDrivePickerPortDriveID, JSONPointer: "/driveId"}, {Port: onedrive.UIDrivePickerPortDriveName, JSONPointer: "/driveName"},
					},
				},
				{
					ID: "sourceFolder", UnitID: onedrive.UIUnitFolderPicker, Label: "Source folder",
					Description: "Choose the folder that directly contains the source file; leave blank to look in the root of the source drive.",
					Bindings: []sdkgo.ConnectorUIBinding{
						{Port: onedrive.UIFolderPickerPortDriveID, JSONPointer: "/driveId"},
						{Port: onedrive.UIFolderPickerPortFolderID, JSONPointer: "/folderId"}, {Port: onedrive.UIFolderPickerPortFolderName, JSONPointer: "/folderName"},
					},
				},
			}},
			Connection: flow.connection, MapToOperationInput: flow.MapToSearchFilesInput,
			Found:    sdkgo.GoTo(selectSourceFile{}),
			NotFound: sdkgo.GoTo(reportSourceNotFound{}),
		})),
		dex.DefineStep(selectSourceFile{}),
		dex.DefineStep(reportSourceNotFound{}),
		dex.DefineStep(onedrive.NewCreateFolderStep(onedrive.CreateFolderStepConfig[CopyFolderRequest]{
			StepType: ensureCopyFolderStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "onedrive", GroupLabel: "OneDrive and SharePoint",
				Explanation: "Ensure the copy folder exists in the destination folder, reusing one that already exists.",
			},
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{
				{
					ID: "destinationSite", UnitID: onedrive.UIUnitSitePicker, Label: "Destination SharePoint site",
					Description: "Choose the SharePoint site whose document library receives the copy folder, so the destination drive picker can list its libraries; leave blank to copy into your own OneDrive.",
					Bindings: []sdkgo.ConnectorUIBinding{
						{Port: onedrive.UISitePickerPortSiteID, JSONPointer: "/siteId"}, {Port: onedrive.UISitePickerPortSiteName, JSONPointer: "/siteName"},
					},
				},
				{
					ID: "destinationDrive", UnitID: onedrive.UIUnitDrivePicker, Label: "Destination drive",
					Description: "Choose your OneDrive or a document library of the destination site; leave blank to copy into your own OneDrive.",
					Bindings: []sdkgo.ConnectorUIBinding{
						{Port: onedrive.UIDrivePickerPortSiteID, JSONPointer: "/siteId"},
						{Port: onedrive.UIDrivePickerPortDriveID, JSONPointer: "/driveId"}, {Port: onedrive.UIDrivePickerPortDriveName, JSONPointer: "/driveName"},
					},
				},
				{
					ID: "destinationFolder", UnitID: onedrive.UIUnitFolderPicker, Label: "Destination folder",
					Description: "Choose the folder in which the copy folder is ensured; leave blank to ensure it in the root of the destination drive.",
					Bindings: []sdkgo.ConnectorUIBinding{
						{Port: onedrive.UIFolderPickerPortDriveID, JSONPointer: "/driveId"},
						{Port: onedrive.UIFolderPickerPortFolderID, JSONPointer: "/folderId"}, {Port: onedrive.UIFolderPickerPortFolderName, JSONPointer: "/folderName"},
					},
				},
			}},
			Connection: flow.connection, MapToOperationInput: flow.MapToCreateFolderInput,
			Created: sdkgo.GoTo(recordCopyFolder{}),
		})),
		dex.DefineStep(recordCopyFolder{}),
		dex.DefineStep(onedrive.NewReadFileTextStep(onedrive.ReadFileTextStepConfig[onedrive.FileSummary]{
			StepType: readSourceStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "onedrive", GroupLabel: "OneDrive and SharePoint",
				Explanation: "Download the resolved source file as bounded UTF-8 text without exposing its download link.",
			},
			Connection: flow.connection, MapToOperationInput: flow.MapToReadFileTextInput,
			Read: sdkgo.GoTo(prepareTextCopy{}),
		})),
		dex.DefineStep(prepareTextCopy{}),
		dex.DefineStep(onedrive.NewUploadFileStep(onedrive.UploadFileStepConfig[CopyRequest]{
			StepType: uploadCopyStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "onedrive", GroupLabel: "OneDrive and SharePoint",
				Explanation: "Write the copy by folder and name; a repeated dispatch finds its own identical file instead of writing twice.",
			},
			Connection: flow.connection, MapToOperationInput: flow.MapToUploadFileInput,
			Uploaded:        sdkgo.GoTo(sdkgo.StepRef[onedrive.UploadFileResult](readBackCopyStepType)),
			AlreadyExists:   sdkgo.GoTo(reportCopyAlreadyExists{}),
			ResultAttribute: &copyUploadResultAttribute,
		})),
		dex.DefineStep(reportCopyAlreadyExists{}),
		dex.DefineStep(onedrive.NewGetFileStep(onedrive.GetFileStepConfig[onedrive.UploadFileResult]{
			StepType: readBackCopyStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "onedrive", GroupLabel: "OneDrive and SharePoint",
				Explanation: "Read the copy's metadata back from Microsoft Graph to confirm it exists.",
			},
			Connection: flow.connection, MapToOperationInput: flow.MapToGetFileInput,
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
// dex:field attribute-key:onedrive-text-copy-request value-type:json editable:false description:"Requested source, copy folder, and copy names"
// dex:field attribute-key:onedrive-text-copy-outcome value-type:json editable:false description:"Text copy outcome"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, outcome, err := textCopyInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"onedrive-text-copy-request": request,
		"onedrive-text-copy-outcome": outcome,
	}}, nil
}

// GetDexDisplay returns the request and outcome.
//
// dex:field attribute-key:onedrive-text-copy-request value-type:json editable:false description:"Source name, copy folder, copy name, and replace choice"
// dex:field attribute-key:onedrive-text-copy-outcome value-type:json editable:false description:"Resolved source, copy folder, and uploaded copy"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, outcome, err := textCopyInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"onedrive-text-copy-request": request,
		"onedrive-text-copy-outcome": outcome,
	}}, nil
}

// MapToSearchFilesInput looks up the exact source name in the saved source folder.
func (flow *Flow) MapToSearchFilesInput(input Input) onedrive.SearchFilesInput {
	location := flow.sourceLocation.Value
	return onedrive.SearchFilesInput{
		DriveID: location.DriveID, ParentFolderID: rootWhenBlank(location.FolderID), Name: input.SourceName,
		NameMatch: onedrive.NameMatchExact,
	}
}

// MapToCreateFolderInput ensures the copy folder in the saved destination folder.
func (flow *Flow) MapToCreateFolderInput(request CopyFolderRequest) onedrive.CreateFolderInput {
	location := flow.destinationLocation.Value
	return onedrive.CreateFolderInput{DriveID: location.DriveID, ParentFolderID: location.FolderID, Name: request.Name}
}

// MapToReadFileTextInput reads the resolved source file from the source drive.
func (flow *Flow) MapToReadFileTextInput(source onedrive.FileSummary) onedrive.ReadFileTextInput {
	return onedrive.ReadFileTextInput{DriveID: flow.sourceLocation.Value.DriveID, ItemID: source.ID}
}

// MapToUploadFileInput writes the copy into the ensured copy folder of the destination drive.
func (flow *Flow) MapToUploadFileInput(request CopyRequest) onedrive.UploadFileInput {
	return onedrive.UploadFileInput{
		DriveID: flow.destinationLocation.Value.DriveID, ParentFolderID: request.FolderID, Name: request.Name,
		ConflictBehavior: request.ConflictBehavior, MimeType: request.MimeType, TextContent: request.Text,
	}
}

// MapToGetFileInput reads back the uploaded copy.
func (flow *Flow) MapToGetFileInput(result onedrive.UploadFileResult) onedrive.GetFileInput {
	return onedrive.GetFileInput{DriveID: flow.destinationLocation.Value.DriveID, ItemID: result.Value.Item.ID}
}

// rootWhenBlank keeps searchFiles in folder mode, which looks a name up by path, for the drive root.
func rootWhenBlank(folderID string) string {
	if folderID == "" {
		return "root"
	}
	return folderID
}

// copyMimeType keeps the source's media type without parameters, or text/plain for a generic label.
func copyMimeType(sourceMimeType string) string {
	mediaType, _, err := mime.ParseMediaType(sourceMimeType)
	if err != nil || mediaType == "application/octet-stream" {
		return fallbackTextMimeType
	}
	return strings.ToLower(mediaType)
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
// dex:explanation text:"Validate and record the requested source, copy folder, and copy names."
type recordCopyRequest struct {
	dex.StepDefaults
}

func (recordCopyRequest) GetStepType() string { return recordRequestStepType }

// WaitFor skips immediately because Dex Web invokes the start Step's WaitFor.
func (recordCopyRequest) WaitFor(dex.Context, Input) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (recordCopyRequest) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	input.SourceName = strings.TrimSpace(input.SourceName)
	input.CopyFolderName = strings.TrimSpace(input.CopyFolderName)
	input.CopyName = strings.TrimSpace(input.CopyName)
	if input.SourceName == "" || input.CopyFolderName == "" || input.CopyName == "" {
		return dex.ForceFail("sourceName, copyFolderName, and copyName are required"), nil
	}
	if err := requestAttribute.Set(ctx, input); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[Input](findSourceStepType), input), nil
}

// dex:group group-id:text-copy group-label:"Text copy"
// dex:explanation text:"Record the one file the path lookup found and ensure the copy folder."
type selectSourceFile struct {
	dex.StepDefaultsNoWaitFor[onedrive.SearchFilesResult]
}

func (selectSourceFile) GetStepType() string { return selectSourceStepType }

func (selectSourceFile) Execute(ctx dex.Context, result onedrive.SearchFilesResult) (*dex.StepDecision, error) {
	// A folder holds at most one item per name, so the path lookup finds exactly one.
	if len(result.Value.Files) != 1 || result.Value.Files[0].IsFolder {
		return dex.ForceFail("the source name does not identify one file"), nil
	}
	request, err := requestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	source := result.Value.Files[0]
	outcome := Outcome{Source: &source}
	if err := outcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[CopyFolderRequest](ensureCopyFolderStepType), CopyFolderRequest{Name: request.CopyFolderName}), nil
}

// dex:group group-id:text-copy group-label:"Text copy"
// dex:explanation text:"Complete with sourceNotFound when the source folder has no item with the name."
type reportSourceNotFound struct {
	dex.StepDefaultsNoWaitFor[onedrive.SearchFilesResult]
}

func (reportSourceNotFound) GetStepType() string { return reportNotFoundStepType }

func (reportSourceNotFound) Execute(ctx dex.Context, _ onedrive.SearchFilesResult) (*dex.StepDecision, error) {
	outcome := Outcome{Status: StatusSourceNotFound}
	if err := outcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

// dex:group group-id:text-copy group-label:"Text copy"
// dex:explanation text:"Record the ensured copy folder and read the source text."
type recordCopyFolder struct {
	dex.StepDefaultsNoWaitFor[onedrive.CreateFolderResult]
}

func (recordCopyFolder) GetStepType() string { return recordCopyFolderStepType }

func (recordCopyFolder) Execute(ctx dex.Context, result onedrive.CreateFolderResult) (*dex.StepDecision, error) {
	outcome, err := outcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	copyFolder := result.Value.Folder
	outcome.CopyFolder = &copyFolder
	outcome.IsCopyFolderExisting = result.Value.IsExistingFolder
	if err := outcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[onedrive.FileSummary](readSourceStepType), *outcome.Source), nil
}

// dex:group group-id:text-copy group-label:"Text copy"
// dex:explanation text:"Record the text size and prepare the upload with fail or replace conflict behavior."
type prepareTextCopy struct {
	dex.StepDefaultsNoWaitFor[onedrive.ReadFileTextResult]
}

func (prepareTextCopy) GetStepType() string { return prepareCopyStepType }

func (prepareTextCopy) Execute(ctx dex.Context, result onedrive.ReadFileTextResult) (*dex.StepDecision, error) {
	request, err := requestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome, err := outcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome.TextByteCount = result.Value.ByteCount
	if err := outcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	conflictBehavior := onedrive.ConflictBehaviorFail
	if request.ShouldReplaceExisting {
		conflictBehavior = onedrive.ConflictBehaviorReplace
	}
	copyRequest := CopyRequest{
		FolderID: outcome.CopyFolder.ID, Name: request.CopyName, MimeType: copyMimeType(result.Value.MimeType),
		Text: result.Value.Text, ConflictBehavior: conflictBehavior,
	}
	return dex.GoTo(sdkgo.StepRef[CopyRequest](uploadCopyStepType), copyRequest), nil
}

// dex:group group-id:text-copy group-label:"Text copy"
// dex:explanation text:"Complete with copyAlreadyExists when a different item already has the copy's name."
type reportCopyAlreadyExists struct {
	dex.StepDefaultsNoWaitFor[onedrive.UploadFileResult]
}

func (reportCopyAlreadyExists) GetStepType() string { return reportCopyExistsStepType }

func (reportCopyAlreadyExists) Execute(ctx dex.Context, result onedrive.UploadFileResult) (*dex.StepDecision, error) {
	outcome, err := outcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	existingCopy := result.Value.Item
	outcome.Status = StatusCopyAlreadyExists
	outcome.ExistingCopy = &existingCopy
	if err := outcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

// dex:group group-id:text-copy group-label:"Text copy"
// dex:explanation text:"Record the read-back copy and complete the Flow."
type completeTextCopy struct {
	dex.StepDefaultsNoWaitFor[onedrive.GetFileResult]
}

func (completeTextCopy) GetStepType() string { return completeCopyStepType }

func (completeTextCopy) Execute(ctx dex.Context, result onedrive.GetFileResult) (*dex.StepDecision, error) {
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
	outcome.IsCopyExistingIdentical = uploadResult.Value.IsExistingFileIdentical
	if err := outcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
