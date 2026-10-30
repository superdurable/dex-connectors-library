// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package onedrive_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/onedrive"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/onedrive/internal/graphfake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func isFolderCreation(request graphfake.Request) bool {
	return request.Method == http.MethodPost && strings.HasSuffix(request.Path, "/children")
}

func TestCreateFolderPostsWithFailConflictBehaviorInBodyAndURL(t *testing.T) {
	graph := newGraph(t)
	client := newGraphClient(t, graph.URL)
	result, err := sdkgo.RunMutation(newDexContext("folder-new"), client.CreateFolder(), graphConnection, onedrive.CreateFolderInput{DriveID: teamDriveID, Name: "Dex copies"})
	require.NoError(t, err)
	require.Equal(t, onedrive.CreateFolderBranchCreated, result.Branch)
	require.False(t, result.Value.IsExistingFolder)
	require.True(t, result.Value.Folder.IsFolder)
	require.Equal(t, result.Value.Folder.ID, result.Receipt.ProviderObjectID)
	requireSecretFree(t, result)

	request := graph.Requests()[0]
	require.Equal(t, "/v1.0/drives/"+teamDriveID+"/root/children", request.Path)
	require.Equal(t, "fail", request.Query.Get("@microsoft.graph.conflictBehavior"))
	var body map[string]any
	require.NoError(t, json.Unmarshal(request.Body, &body))
	require.Equal(t, map[string]any{"name": "Dex copies", "folder": map[string]any{}, "@microsoft.graph.conflictBehavior": "fail"}, body)
}

func TestCreateFolderReturnsAnExistingFolderAndReportsAFileWithTheName(t *testing.T) {
	graph := newGraph(t)
	existingID := graph.AddItem(graphfake.Item{Name: "Dex copies", DriveID: teamDriveID, ParentID: "root", IsFolder: true})
	graph.AddItem(graphfake.Item{Name: "notes", DriveID: teamDriveID, ParentID: "root", MimeType: "text/plain", Content: []byte("x")})
	client := newGraphClient(t, graph.URL)

	existing, err := sdkgo.RunMutation(newDexContext("folder-existing"), client.CreateFolder(), graphConnection, onedrive.CreateFolderInput{DriveID: teamDriveID, Name: "dex COPIES"})
	require.NoError(t, err)
	require.Equal(t, onedrive.CreateFolderBranchCreated, existing.Branch)
	require.True(t, existing.Value.IsExistingFolder)
	require.Equal(t, existingID, existing.Value.Folder.ID)

	conflict, err := sdkgo.RunMutation(newDexContext("folder-file-conflict"), client.CreateFolder(), graphConnection, onedrive.CreateFolderInput{DriveID: teamDriveID, Name: "notes"})
	require.NoError(t, err)
	require.Equal(t, onedrive.CreateFolderBranchNameConflict, conflict.Branch)
	require.False(t, conflict.Value.Folder.IsFolder)
	require.Equal(t, sdkgo.FailureConflict, conflict.Failure.Kind)
	require.Equal(t, 2, graph.ItemCount(), "neither call created an item")
}

func TestCreateFolderRetriesALostResponseAndFindsTheFolderItCreated(t *testing.T) {
	graph := newGraph(t)
	var creations atomic.Int32
	graph.InterceptAfterApply(func(request graphfake.Request) *graphfake.Response {
		if isFolderCreation(request) && creations.Add(1) == 1 {
			return &graphfake.Response{ShouldDropConnection: true}
		}
		return nil
	})
	client := newGraphClient(t, graph.URL)
	ctx := newDexContext("folder-lost")
	input := onedrive.CreateFolderInput{DriveID: teamDriveID, Name: "Dex copies"}
	_, err := sdkgo.RunMutation(ctx, client.CreateFolder(), graphConnection, input)
	require.Error(t, err)
	recovered, err := sdkgo.RunMutation(ctx, client.CreateFolder(), graphConnection, input)
	require.NoError(t, err)
	require.Equal(t, onedrive.CreateFolderBranchCreated, recovered.Branch)
	require.True(t, recovered.Value.IsExistingFolder)
	require.Len(t, graph.ItemsNamed(teamDriveID, "root", "Dex copies"), 1)
}

func TestCreateFolderRejectsInvalidNamesAndClassifiesRejections(t *testing.T) {
	graph := newGraph(t)
	client := newGraphClient(t, graph.URL)
	for _, name := range []string{"", "Reports.", "a/b", "~tmp", "a#1"} {
		result, err := sdkgo.RunMutation(newDexContext("folder-invalid"), client.CreateFolder(), graphConnection, onedrive.CreateFolderInput{DriveID: teamDriveID, Name: name})
		require.NoError(t, err)
		require.Equal(t, onedrive.CreateFolderBranchDefect, result.Branch, name)
	}
	require.Empty(t, graph.Requests())

	missingParent, err := sdkgo.RunMutation(newDexContext("folder-missing-parent"), client.CreateFolder(), graphConnection, onedrive.CreateFolderInput{DriveID: teamDriveID, ParentFolderID: "01NOFOLDER", Name: "x"})
	require.NoError(t, err)
	require.Equal(t, onedrive.CreateFolderBranchProviderRejected, missingParent.Branch)

	graph.Intercept(func(request graphfake.Request) *graphfake.Response {
		return &graphfake.Response{StatusCode: http.StatusServiceUnavailable, Code: "serviceNotAvailable", Header: http.Header{"Retry-After": {"2"}}}
	})
	_, err = sdkgo.RunMutation(newDexContext("folder-unavailable"), client.CreateFolder(), graphConnection, onedrive.CreateFolderInput{DriveID: teamDriveID, Name: "x"})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "GRAPH-MESSAGE-SENTINEL")
}
