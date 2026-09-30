// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package textcopy

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/google/drive"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func newTestFlow(sourceFolderID string, destinationFolderID string) *Flow {
	return NewFlow(drive.Connection{},
		sdkgo.ConnectorLoadedConfiguration[FolderConfiguration]{Reference: SourceFolderConfigurationRef(), Value: FolderConfiguration{FolderID: sourceFolderID}},
		sdkgo.ConnectorLoadedConfiguration[FolderConfiguration]{Reference: DestinationFolderConfigurationRef(), Value: FolderConfiguration{FolderID: destinationFolderID}},
	)
}

func TestMapToSearchFilesInputResolvesTheExactNameInTheSavedFolder(t *testing.T) {
	require.Equal(t, drive.SearchFilesInput{
		Name: "Ops Policy", NameMatch: drive.NameMatchExact, ParentFolderID: "fld_ops", PageSize: sourceCandidatesPageSize,
	}, newTestFlow("fld_ops", "").MapToSearchFilesInput(Input{SourceName: "Ops Policy", CopyName: "copy.txt"}))
	require.Empty(t, newTestFlow("", "").MapToSearchFilesInput(Input{SourceName: "Ops Policy"}).ParentFolderID)
}

func TestMapToUploadFileInputUploadsTheTextIntoTheSavedFolder(t *testing.T) {
	require.Equal(t, drive.UploadFileInput{
		Name: "copy.csv", ParentFolderID: "fld_finance", MimeType: "text/csv", TextContent: "a,b\n",
	}, newTestFlow("", "fld_finance").MapToUploadFileInput(CopyRequest{Name: "copy.csv", MimeType: "text/csv", Text: "a,b\n"}))
	require.Empty(t, newTestFlow("", "").MapToUploadFileInput(CopyRequest{Name: "copy.txt", MimeType: "text/plain"}).ParentFolderID)
}

func TestConnectorMappersForwardFileIDs(t *testing.T) {
	require.Equal(t, drive.ReadFileTextInput{FileID: "file_policy"}, MapToReadFileTextInput(drive.FileSummary{ID: "file_policy"}))
	require.Equal(t, drive.GetFileInput{FileID: "created_1"}, MapToGetFileInput(drive.UploadFileResult{Value: drive.UploadFileOutput{File: drive.File{ID: "created_1"}}}))
}

func TestFolderConfigurationRefsMatchTheirSteps(t *testing.T) {
	require.Equal(t, sdkgo.ConnectorConfigurationRef{
		ConnectorID: drive.ConnectorID, ConnectionName: ConnectionName, OperationID: "searchFiles", FlowType: FlowType, StepType: findSourceStepType,
	}, SourceFolderConfigurationRef())
	require.Equal(t, sdkgo.ConnectorConfigurationRef{
		ConnectorID: drive.ConnectorID, ConnectionName: ConnectionName, OperationID: "uploadFile", FlowType: FlowType, StepType: uploadCopyStepType,
	}, DestinationFolderConfigurationRef())
}

func TestStepIdentitiesMatchTheFlowDefinition(t *testing.T) {
	require.Equal(t, FlowType, dex.GetFinalFlowType(newTestFlow("", "")))
	require.Equal(t, recordRequestStepType, dex.GetFinalStepType[Input](recordCopyRequest{}))
	require.Equal(t, selectSourceStepType, dex.GetFinalStepType[drive.SearchFilesResult](selectSourceFile{}))
	require.Equal(t, reportNotFoundStepType, dex.GetFinalStepType[drive.SearchFilesResult](reportSourceNotFound{}))
	require.Equal(t, prepareCopyStepType, dex.GetFinalStepType[drive.ReadFileTextResult](prepareTextCopy{}))
	require.Equal(t, completeCopyStepType, dex.GetFinalStepType[drive.GetFileResult](completeTextCopy{}))
	wait, err := recordCopyRequest{}.WaitFor(nil, Input{})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
}

func TestFlowRegistersOnlyWithItsStaticConnection(t *testing.T) {
	client, err := drive.New(drive.Config{}, sdkgo.StaticCredentialProvider[drive.Credentials]{})
	require.NoError(t, err)
	connection, err := drive.NewConnection(client, sdkgo.ConnectionRef{Provider: "google", Name: ConnectionName})
	require.NoError(t, err)
	unset := sdkgo.ConnectorLoadedConfiguration[FolderConfiguration]{}
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(connection, unset, unset)})
	require.NoError(t, err)

	otherConnection, err := drive.NewConnection(client, sdkgo.ConnectionRef{Provider: "google", Name: "another-connection"})
	require.NoError(t, err)
	require.Panics(t, func() { _, _ = dex.NewRegistry([]dex.Flow{NewFlow(otherConnection, unset, unset)}) })
}
