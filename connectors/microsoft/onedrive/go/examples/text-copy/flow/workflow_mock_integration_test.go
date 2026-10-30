//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package textcopy

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/onedrive/go"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/onedrive/go/onedrivemock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestTextCopyUsesMockDefaultsWithRealDex(t *testing.T) {
	mock := onedrivemock.New(t, ConnectionName)
	harness := newTextCopyHarnessWithConnection(t, mock.Connection(), "source-folder", "destination-folder")
	input := Input{SourceName: " Ops Policy.txt ", CopyFolderName: "Dex copies", CopyName: "Ops Policy (copy).txt"}
	run := harness.run(t, "onedrive-mock", input)
	require.Equal(t, dex.FlowCompleted, run.status, run.errorMessage)
	require.Equal(t, StatusCopied, run.outcome.Status)
	require.Equal(t, int64(len(sourceText)), run.outcome.TextByteCount)
	require.Equal(t, "source-file-1", run.outcome.Source.ID)
	require.Equal(t, "copy-folder-1", run.outcome.CopyFolder.ID)
	require.Equal(t, "copy-file-1", run.outcome.Copy.ID)

	searches := mock.SearchFiles().Calls()
	require.Len(t, searches, 1)
	require.Equal(t, onedrive.SearchFilesInput{DriveID: sourceDriveID, ParentFolderID: "source-folder", Name: "Ops Policy.txt", NameMatch: onedrive.NameMatchExact}, searches[0].Input)
	folders := mock.CreateFolder().Calls()
	require.Len(t, folders, 1)
	require.Equal(t, onedrive.CreateFolderInput{DriveID: destinationDrive, ParentFolderID: "destination-folder", Name: "Dex copies"}, folders[0].Input)
	reads := mock.ReadFileText().Calls()
	require.Len(t, reads, 1)
	require.Equal(t, onedrive.ReadFileTextInput{DriveID: sourceDriveID, ItemID: "source-file-1"}, reads[0].Input)
	uploads := mock.UploadFile().Calls()
	require.Len(t, uploads, 1)
	require.Equal(t, onedrive.UploadFileInput{DriveID: destinationDrive, ParentFolderID: "copy-folder-1", Name: "Ops Policy (copy).txt", ConflictBehavior: onedrive.ConflictBehaviorFail, MimeType: "text/plain", TextContent: sourceText}, uploads[0].Input)
	require.Equal(t, onedrive.UploadFileBranchUploaded, uploads[0].Branch)
	metadata := mock.GetFile().Calls()
	require.Len(t, metadata, 1)
	require.Equal(t, onedrive.GetFileInput{DriveID: destinationDrive, ItemID: "copy-file-1"}, metadata[0].Input)
}
