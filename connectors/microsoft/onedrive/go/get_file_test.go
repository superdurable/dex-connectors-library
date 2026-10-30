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

func TestGetFileReturnsMetadataWithoutTheDownloadURL(t *testing.T) {
	graph := newGraph(t)
	fileID := graph.AddItem(graphfake.Item{Name: "Q3 report.csv", DriveID: teamDriveID, ParentID: "root", MimeType: "text/csv", Content: []byte("a,b\n1,2\n")})
	client := newGraphClient(t, graph.URL)

	result, err := sdkgo.RunQuery(newDexContext("get"), client.GetFile(), graphConnection, onedrive.GetFileInput{DriveID: teamDriveID, ItemID: fileID})
	require.NoError(t, err)
	require.Equal(t, onedrive.GetFileBranchFound, result.Branch)
	item := result.Value
	require.Equal(t, fileID, item.ID)
	require.Equal(t, "Q3 report.csv", item.Name)
	require.Equal(t, teamDriveID, item.DriveID)
	require.Equal(t, "business", item.DriveType)
	require.False(t, item.IsFolder)
	require.Equal(t, "text/csv", item.MimeType)
	require.Equal(t, int64(8), item.SizeBytes)
	require.Equal(t, graphfake.QuickXorHash([]byte("a,b\n1,2\n")), item.QuickXorHash)
	require.Equal(t, "Megan Bowen", item.CreatedBy.DisplayName)
	require.Equal(t, fileID, result.Receipt.ProviderObjectID)
	require.Equal(t, "fake-request-1", result.Receipt.ProviderRequestID)
	request := graph.Requests()[0]
	require.Equal(t, "/v1.0/drives/"+teamDriveID+"/items/"+fileID, request.Path)
	require.NotContains(t, request.Query.Get("$select"), "downloadUrl")
	requireSecretFree(t, result)
}

func TestGetFileReadsTheDriveRootAndReportsAMissingItem(t *testing.T) {
	graph := newGraph(t)
	client := newGraphClient(t, graph.URL)
	root, err := sdkgo.RunQuery(newDexContext("get-root"), client.GetFile(), graphConnection, onedrive.GetFileInput{ItemID: "root"})
	require.NoError(t, err)
	require.Equal(t, onedrive.GetFileBranchFound, root.Branch)
	require.True(t, root.Value.IsFolder)
	require.Equal(t, "/v1.0/me/drive/root", graph.Requests()[0].Path)

	missing, err := sdkgo.RunQuery(newDexContext("get-missing"), client.GetFile(), graphConnection, onedrive.GetFileInput{ItemID: "01MISSING"})
	require.NoError(t, err)
	require.Equal(t, onedrive.GetFileBranchNotFound, missing.Branch)
	require.Equal(t, sdkgo.FailureNotFound, missing.Failure.Kind)
	requireSecretFree(t, missing)
}

func TestGetFileRejectsInvalidIDsAndClassifiesDenial(t *testing.T) {
	graph := newGraph(t)
	client := newGraphClient(t, graph.URL)
	for _, input := range []onedrive.GetFileInput{{}, {ItemID: "a/b"}, {DriveID: "b!x y", ItemID: "01A"}} {
		result, err := sdkgo.RunQuery(newDexContext("get-invalid"), client.GetFile(), graphConnection, input)
		require.NoError(t, err)
		require.Equal(t, onedrive.GetFileBranchDefect, result.Branch)
	}
	require.Empty(t, graph.Requests())

	graph.Intercept(func(graphfake.Request) *graphfake.Response {
		return &graphfake.Response{StatusCode: http.StatusForbidden, Code: "accessDenied"}
	})
	denied, err := sdkgo.RunQuery(newDexContext("get-denied"), client.GetFile(), graphConnection, onedrive.GetFileInput{ItemID: "01A"})
	require.NoError(t, err)
	require.Equal(t, onedrive.GetFileBranchProviderRejected, denied.Branch)
	require.Equal(t, sdkgo.FailureAuthorization, denied.Failure.Kind)
	requireSecretFree(t, denied)
}
