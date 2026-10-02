// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package textcopy

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/onedrive"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func newTestFlow(source LocationConfiguration, destination LocationConfiguration) *Flow {
	return NewFlow(onedrive.Connection{},
		sdkgo.ConnectorLoadedConfiguration[LocationConfiguration]{Reference: SourceLocationConfigurationRef(), Value: source},
		sdkgo.ConnectorLoadedConfiguration[LocationConfiguration]{Reference: DestinationLocationConfigurationRef(), Value: destination},
	)
}

func TestMapToSearchFilesInputLooksUpTheExactNameInTheSavedFolder(t *testing.T) {
	flow := newTestFlow(LocationConfiguration{DriveID: "b!team", FolderID: "01POLICIES"}, LocationConfiguration{})
	require.Equal(t, onedrive.SearchFilesInput{
		DriveID: "b!team", ParentFolderID: "01POLICIES", Name: "Ops Policy.txt", NameMatch: onedrive.NameMatchExact,
	}, flow.MapToSearchFilesInput(Input{SourceName: "Ops Policy.txt"}))
	require.Equal(t, "root", newTestFlow(LocationConfiguration{}, LocationConfiguration{}).MapToSearchFilesInput(Input{SourceName: "a"}).ParentFolderID,
		"a blank folder still looks the name up by path, in the drive root")
}

func TestDestinationMappersUseTheDestinationDriveAndTheEnsuredFolder(t *testing.T) {
	flow := newTestFlow(LocationConfiguration{DriveID: "b!source"}, LocationConfiguration{DriveID: "b!finance", FolderID: "01REPORTS"})
	require.Equal(t, onedrive.CreateFolderInput{DriveID: "b!finance", ParentFolderID: "01REPORTS", Name: "Dex copies"},
		flow.MapToCreateFolderInput(CopyFolderRequest{Name: "Dex copies"}))
	require.Equal(t, onedrive.ReadFileTextInput{DriveID: "b!source", ItemID: "01SOURCE"}, flow.MapToReadFileTextInput(onedrive.FileSummary{ID: "01SOURCE"}))
	require.Equal(t, onedrive.UploadFileInput{
		DriveID: "b!finance", ParentFolderID: "01COPIES", Name: "copy.txt", ConflictBehavior: onedrive.ConflictBehaviorFail,
		MimeType: "text/plain", TextContent: "text",
	}, flow.MapToUploadFileInput(CopyRequest{FolderID: "01COPIES", Name: "copy.txt", MimeType: "text/plain", Text: "text", ConflictBehavior: onedrive.ConflictBehaviorFail}))
	require.Equal(t, onedrive.GetFileInput{DriveID: "b!finance", ItemID: "01COPY"},
		flow.MapToGetFileInput(onedrive.UploadFileResult{Value: onedrive.UploadFileOutput{Item: onedrive.DriveItem{ID: "01COPY"}}}))
}

func TestCopyMimeTypeDropsParametersAndGenericLabels(t *testing.T) {
	require.Equal(t, "text/csv", copyMimeType("text/csv"))
	require.Equal(t, "text/plain", copyMimeType("text/plain; charset=utf-8"))
	require.Equal(t, "text/plain", copyMimeType("application/octet-stream"))
	require.Equal(t, "text/plain", copyMimeType(""))
}

func TestLocationConfigurationRefsMatchTheirSteps(t *testing.T) {
	require.Equal(t, sdkgo.ConnectorConfigurationRef{
		ConnectorID: onedrive.ConnectorID, ConnectionName: ConnectionName, OperationID: "searchFiles", FlowType: FlowType, StepType: findSourceStepType,
	}, SourceLocationConfigurationRef())
	require.Equal(t, sdkgo.ConnectorConfigurationRef{
		ConnectorID: onedrive.ConnectorID, ConnectionName: ConnectionName, OperationID: "createFolder", FlowType: FlowType, StepType: ensureCopyFolderStepType,
	}, DestinationLocationConfigurationRef())
}

func TestStepIdentitiesMatchTheFlowDefinition(t *testing.T) {
	require.Equal(t, FlowType, dex.GetFinalFlowType(newTestFlow(LocationConfiguration{}, LocationConfiguration{})))
	require.Equal(t, recordRequestStepType, dex.GetFinalStepType[Input](recordCopyRequest{}))
	require.Equal(t, selectSourceStepType, dex.GetFinalStepType[onedrive.SearchFilesResult](selectSourceFile{}))
	require.Equal(t, reportNotFoundStepType, dex.GetFinalStepType[onedrive.SearchFilesResult](reportSourceNotFound{}))
	require.Equal(t, recordCopyFolderStepType, dex.GetFinalStepType[onedrive.CreateFolderResult](recordCopyFolder{}))
	require.Equal(t, prepareCopyStepType, dex.GetFinalStepType[onedrive.ReadFileTextResult](prepareTextCopy{}))
	require.Equal(t, reportCopyExistsStepType, dex.GetFinalStepType[onedrive.UploadFileResult](reportCopyAlreadyExists{}))
	require.Equal(t, completeCopyStepType, dex.GetFinalStepType[onedrive.GetFileResult](completeTextCopy{}))
	wait, err := recordCopyRequest{}.WaitFor(nil, Input{})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
}

func TestFlowRegistersOnlyWithItsStaticConnection(t *testing.T) {
	client, err := onedrive.New(onedrive.Config{}, sdkgo.StaticCredentialProvider[onedrive.Credentials]{})
	require.NoError(t, err)
	connection, err := onedrive.NewConnection(client, sdkgo.ConnectionRef{Provider: "microsoft", Name: ConnectionName})
	require.NoError(t, err)
	unset := sdkgo.ConnectorLoadedConfiguration[LocationConfiguration]{}
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(connection, unset, unset)})
	require.NoError(t, err)

	otherConnection, err := onedrive.NewConnection(client, sdkgo.ConnectionRef{Provider: "microsoft", Name: "another-connection"})
	require.NoError(t, err)
	require.Panics(t, func() { _, _ = dex.NewRegistry([]dex.Flow{NewFlow(otherConnection, unset, unset)}) })
}
