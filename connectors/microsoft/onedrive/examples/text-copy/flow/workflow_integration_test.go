//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package textcopy

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/onedrive/internal/graphfake"
	"github.com/superdurable/dex/sdk-go/dex"
)

func isCopyUpload(request graphfake.Request) bool {
	return request.Method == http.MethodPut && strings.HasSuffix(request.Path, ":/content")
}

func TestTextCopyExampleRoutesEveryBusinessOutcomeWithRealDex(t *testing.T) {
	graph := newGraphFixture(t)
	harness := newTextCopyHarness(t, graph)
	input := Input{SourceName: "Ops Policy.txt", CopyFolderName: "Dex copies", CopyName: "Ops Policy (copy).txt"}

	copied := harness.run(t, "copied", input)
	require.Equal(t, dex.FlowCompleted, copied.status)
	require.Equal(t, StatusCopied, copied.outcome.Status)
	require.False(t, copied.outcome.IsCopyFolderExisting)
	require.False(t, copied.outcome.IsCopyExistingIdentical)
	require.Equal(t, int64(len(sourceText)), copied.outcome.TextByteCount)
	copyFolders := graph.ItemsNamed(destinationDrive, graph.reportsFolderID, "Dex copies")
	require.Len(t, copyFolders, 1)
	require.Equal(t, copyFolders[0].ID, copied.outcome.CopyFolder.ID)
	copies := graph.ItemsNamed(destinationDrive, copyFolders[0].ID, "Ops Policy (copy).txt")
	require.Len(t, copies, 1)
	require.Equal(t, sourceText, string(copies[0].Content), "the source folder's file was read, not the root decoy")
	require.Equal(t, "text/plain", copies[0].MimeType)
	require.Equal(t, copies[0].ID, copied.outcome.Copy.ID)

	repeated := harness.run(t, "repeated", input)
	require.Equal(t, StatusCopied, repeated.outcome.Status, "a second Flow converges on the identical copy")
	require.True(t, repeated.outcome.IsCopyFolderExisting)
	require.True(t, repeated.outcome.IsCopyExistingIdentical)
	require.Len(t, graph.ItemsNamed(destinationDrive, copyFolders[0].ID, "Ops Policy (copy).txt"), 1)

	graph.AddItem(graphfake.Item{Name: "Ops Policy.txt", DriveID: destinationDrive, ParentID: copyFolders[0].ID, MimeType: "text/plain", Content: []byte("edited by hand")})
	blocked := harness.run(t, "blocked", Input{SourceName: "Ops Policy.txt", CopyFolderName: "Dex copies", CopyName: "Ops Policy.txt"})
	require.Equal(t, StatusCopyAlreadyExists, blocked.outcome.Status)
	require.Equal(t, "Ops Policy.txt", blocked.outcome.ExistingCopy.Name)
	require.Equal(t, "edited by hand", string(graph.ItemsNamed(destinationDrive, copyFolders[0].ID, "Ops Policy.txt")[0].Content))

	replaced := harness.run(t, "replaced", Input{SourceName: "Ops Policy.txt", CopyFolderName: "Dex copies", CopyName: "Ops Policy.txt", ShouldReplaceExisting: true})
	require.Equal(t, StatusCopied, replaced.outcome.Status)
	require.Equal(t, sourceText, string(graph.ItemsNamed(destinationDrive, copyFolders[0].ID, "Ops Policy.txt")[0].Content))

	missing := harness.run(t, "missing", Input{SourceName: "Ops Polcy.txt", CopyFolderName: "Never", CopyName: "never.txt"})
	require.Equal(t, StatusSourceNotFound, missing.outcome.Status)
	require.Empty(t, graph.ItemsNamed(destinationDrive, graph.reportsFolderID, "Never"))

	requestsBefore := len(graph.Requests())
	invalid := harness.run(t, "invalid", Input{SourceName: " ", CopyFolderName: "x", CopyName: "y"})
	require.Equal(t, dex.FlowFailed, invalid.status)
	require.Len(t, graph.Requests(), requestsBefore, "an invalid request never reaches Microsoft Graph")
}

func TestTextCopyUnwiredRejectionFailsTheFlowWithoutRetryingWithRealDex(t *testing.T) {
	graph := newGraphFixture(t)
	graph.Intercept(func(request graphfake.Request) *graphfake.Response {
		if isCopyUpload(request) {
			return &graphfake.Response{StatusCode: http.StatusForbidden, Code: "accessDenied"}
		}
		return nil
	})
	harness := newTextCopyHarness(t, graph)
	result := harness.run(t, "denied", Input{SourceName: "Ops Policy.txt", CopyFolderName: "Dex copies", CopyName: "denied.txt"})
	require.Equal(t, dex.FlowFailed, result.status)
	require.Equal(t, 1, graph.CountRequests(http.MethodPut, func(string) bool { return true }), "a conclusive rejection is not retried")
}
