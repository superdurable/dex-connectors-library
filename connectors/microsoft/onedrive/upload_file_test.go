// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package onedrive_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/onedrive"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/onedrive/internal/graphfake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func uploadInput(folderID string, behavior onedrive.ConflictBehavior, text string) onedrive.UploadFileInput {
	return onedrive.UploadFileInput{
		DriveID: teamDriveID, ParentFolderID: folderID, Name: "Jan-2026 reconciliation.csv",
		ConflictBehavior: behavior, MimeType: "text/csv", TextContent: text,
	}
}

func TestUploadFilePutsByPathWithTheExplicitConflictBehavior(t *testing.T) {
	graph := newGraph(t)
	folderID := graph.AddItem(graphfake.Item{Name: "Finance", DriveID: teamDriveID, ParentID: "root", IsFolder: true})
	client := newGraphClient(t, graph.URL)

	result, err := sdkgo.RunMutation(newDexContext("upload-new"), client.UploadFile(), graphConnection, uploadInput(folderID, onedrive.ConflictBehaviorFail, "date,amount\n"))
	require.NoError(t, err)
	require.Equal(t, onedrive.UploadFileBranchUploaded, result.Branch)
	require.False(t, result.Value.IsExistingFileIdentical)
	require.Equal(t, "Jan-2026 reconciliation.csv", result.Value.Item.Name)
	require.Equal(t, folderID, result.Value.Item.ParentFolderID)
	require.Equal(t, result.Value.Item.ID, result.Receipt.ProviderObjectID)
	require.NotEmpty(t, result.Receipt.IdempotencyKey)
	requireSecretFree(t, result)

	request := graph.Requests()[0]
	require.Equal(t, http.MethodPut, request.Method)
	require.Equal(t, "/v1.0/drives/"+teamDriveID+"/items/"+folderID+":/Jan-2026 reconciliation.csv:/content", request.Path)
	require.Equal(t, "fail", request.Query.Get("@microsoft.graph.conflictBehavior"))
	require.Equal(t, "text/csv", request.Header.Get("Content-Type"))
	require.Equal(t, "date,amount\n", string(request.Body))
	stored := graph.ItemsNamed(teamDriveID, folderID, "Jan-2026 reconciliation.csv")
	require.Len(t, stored, 1)
	require.Equal(t, "date,amount\n", string(stored[0].Content))
}

func TestUploadFileToTheDriveRootUsesTheRootPath(t *testing.T) {
	graph := newGraph(t)
	client := newGraphClient(t, graph.URL)
	input := onedrive.UploadFileInput{Name: "pixel.png", ConflictBehavior: onedrive.ConflictBehaviorReplace, MimeType: "image/png", ByteContent: []byte{0x89, 'P', 0x00}}
	result, err := sdkgo.RunMutation(newDexContext("upload-root"), client.UploadFile(), graphConnection, input)
	require.NoError(t, err)
	require.Equal(t, onedrive.UploadFileBranchUploaded, result.Branch)
	require.Equal(t, "/v1.0/me/drive/root:/pixel.png:/content", graph.Requests()[0].Path)
	require.Equal(t, []byte{0x89, 'P', 0x00}, graph.ItemsNamed(myDriveID, "root", "pixel.png")[0].Content)
}

func TestUploadFileFailModeConvergesOnIdenticalContentAndReportsOtherContent(t *testing.T) {
	graph := newGraph(t)
	folderID := graph.AddItem(graphfake.Item{Name: "Finance", DriveID: teamDriveID, ParentID: "root", IsFolder: true})
	existingID := graph.AddItem(graphfake.Item{Name: "Jan-2026 reconciliation.csv", DriveID: teamDriveID, ParentID: folderID, MimeType: "text/csv", Content: []byte("v1\n")})
	client := newGraphClient(t, graph.URL)

	identical, err := sdkgo.RunMutation(newDexContext("upload-identical"), client.UploadFile(), graphConnection, uploadInput(folderID, onedrive.ConflictBehaviorFail, "v1\n"))
	require.NoError(t, err)
	require.Equal(t, onedrive.UploadFileBranchUploaded, identical.Branch)
	require.True(t, identical.Value.IsExistingFileIdentical)
	require.Equal(t, existingID, identical.Value.Item.ID)

	different, err := sdkgo.RunMutation(newDexContext("upload-different"), client.UploadFile(), graphConnection, uploadInput(folderID, onedrive.ConflictBehaviorFail, "v2\n"))
	require.NoError(t, err)
	require.Equal(t, onedrive.UploadFileBranchAlreadyExists, different.Branch)
	require.Equal(t, existingID, different.Value.Item.ID)
	require.Equal(t, sdkgo.FailureConflict, different.Failure.Kind)
	requireSecretFree(t, different)
	require.Equal(t, "v1\n", string(graph.ItemsNamed(teamDriveID, folderID, "Jan-2026 reconciliation.csv")[0].Content), "fail never overwrites")

	graph.AddItem(graphfake.Item{Name: "Archive", DriveID: teamDriveID, ParentID: folderID, IsFolder: true})
	input := uploadInput(folderID, onedrive.ConflictBehaviorFail, "v1\n")
	input.Name = "Archive"
	folderConflict, err := sdkgo.RunMutation(newDexContext("upload-folder-conflict"), client.UploadFile(), graphConnection, input)
	require.NoError(t, err)
	require.Equal(t, onedrive.UploadFileBranchAlreadyExists, folderConflict.Branch)
	require.True(t, folderConflict.Value.Item.IsFolder)
}

func TestUploadFileReplaceModeWritesANewVersionOfTheSameFile(t *testing.T) {
	graph := newGraph(t)
	folderID := graph.AddItem(graphfake.Item{Name: "Finance", DriveID: teamDriveID, ParentID: "root", IsFolder: true})
	existingID := graph.AddItem(graphfake.Item{Name: "Jan-2026 reconciliation.csv", DriveID: teamDriveID, ParentID: folderID, MimeType: "text/csv", Content: []byte("v1\n")})
	client := newGraphClient(t, graph.URL)
	result, err := sdkgo.RunMutation(newDexContext("upload-replace"), client.UploadFile(), graphConnection, uploadInput(folderID, onedrive.ConflictBehaviorReplace, "v2\n"))
	require.NoError(t, err)
	require.Equal(t, onedrive.UploadFileBranchUploaded, result.Branch)
	require.Equal(t, existingID, result.Value.Item.ID)
	require.Equal(t, "replace", graph.Requests()[0].Query.Get("@microsoft.graph.conflictBehavior"))
	require.Equal(t, "v2\n", string(graph.ItemsNamed(teamDriveID, folderID, "Jan-2026 reconciliation.csv")[0].Content))
}
