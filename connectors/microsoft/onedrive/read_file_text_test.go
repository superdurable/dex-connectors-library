// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package onedrive_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/onedrive"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/onedrive/internal/graphfake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestReadFileTextFollowsTheDownloadRedirectWithoutTheAccessToken(t *testing.T) {
	graph := newGraph(t)
	text := "\xef\xbb\xbfRefunds above 500 need approval.\n"
	fileID := graph.AddItem(graphfake.Item{Name: "Ops Policy.txt", DriveID: teamDriveID, ParentID: "root", MimeType: "text/plain", Content: []byte(text)})
	client := newGraphClient(t, graph.URL)

	result, err := sdkgo.RunQuery(newDexContext("read"), client.ReadFileText(), graphConnection, onedrive.ReadFileTextInput{DriveID: teamDriveID, ItemID: fileID})
	require.NoError(t, err)
	require.Equal(t, onedrive.ReadFileTextBranchRead, result.Branch)
	require.Equal(t, text, result.Value.Text, "the byte order mark is file content and is kept")
	require.Equal(t, int64(len(text)), result.Value.ByteCount)
	require.Equal(t, "text/plain", result.Value.MimeType)
	requireSecretFree(t, result)

	requests := graph.Requests()
	require.Len(t, requests, 3)
	require.Equal(t, "/v1.0/drives/"+teamDriveID+"/items/"+fileID, requests[0].Path)
	require.Equal(t, "/v1.0/drives/"+teamDriveID+"/items/"+fileID+"/content", requests[1].Path)
	require.Equal(t, "Bearer "+graphTestToken, requests[1].Header.Get("Authorization"))
	require.Empty(t, requests[1].Header.Get("Accept"))
	require.True(t, strings.HasPrefix(requests[2].Path, "/download/"))
	require.Empty(t, requests[2].Header.Get("Authorization"), "the pre-authenticated URL never receives the token")
}

func TestReadFileTextAcceptsTextByExtensionWhenGraphReportsAGenericType(t *testing.T) {
	graph := newGraph(t)
	fileID := graph.AddItem(graphfake.Item{Name: "README.md", DriveID: myDriveID, ParentID: "root", MimeType: "application/octet-stream", Content: []byte("# Notes\n")})
	client := newGraphClient(t, graph.URL)
	result, err := sdkgo.RunQuery(newDexContext("read-md"), client.ReadFileText(), graphConnection, onedrive.ReadFileTextInput{ItemID: fileID})
	require.NoError(t, err)
	require.Equal(t, onedrive.ReadFileTextBranchRead, result.Branch)
	require.Equal(t, "# Notes\n", result.Value.Text)
}

func TestReadFileTextDecidesUnsupportedAndOversizedFilesBeforeDownloading(t *testing.T) {
	graph := newGraph(t)
	folderID := graph.AddItem(graphfake.Item{Name: "Reports", DriveID: myDriveID, ParentID: "root", IsFolder: true})
	documentID := graph.AddItem(graphfake.Item{Name: "Plan.docx", DriveID: myDriveID, ParentID: "root", MimeType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document", Content: []byte("PK")})
	largeID := graph.AddItem(graphfake.Item{Name: "big.txt", DriveID: myDriveID, ParentID: "root", MimeType: "text/plain", Content: []byte(strings.Repeat("x", 65))})
	client := newGraphClient(t, graph.URL, onedrive.Config{MaxTextBytes: 64})
	for _, test := range []struct {
		itemID     string
		wantBranch sdkgo.BranchID
	}{
		{itemID: folderID, wantBranch: onedrive.ReadFileTextBranchUnsupportedContent},
		{itemID: documentID, wantBranch: onedrive.ReadFileTextBranchUnsupportedContent},
		{itemID: largeID, wantBranch: onedrive.ReadFileTextBranchTooLarge},
	} {
		result, err := sdkgo.RunQuery(newDexContext("read-undownloadable"), client.ReadFileText(), graphConnection, onedrive.ReadFileTextInput{ItemID: test.itemID})
		require.NoError(t, err)
		require.Equal(t, test.wantBranch, result.Branch)
		require.Equal(t, test.itemID, result.Value.ItemID)
		require.Empty(t, result.Value.Text)
	}
	require.Zero(t, graph.CountRequests(http.MethodGet, pathHasSuffix("/content")))
}

func TestReadFileTextRejectsInvalidUTF8AndANULByte(t *testing.T) {
	graph := newGraph(t)
	client := newGraphClient(t, graph.URL)
	for _, content := range [][]byte{{0xff, 0xfe, 'a'}, []byte("a\x00b")} {
		fileID := graph.AddItem(graphfake.Item{Name: "data.txt", DriveID: myDriveID, ParentID: "root", MimeType: "text/plain", Content: content})
		result, err := sdkgo.RunQuery(newDexContext("read-binary"), client.ReadFileText(), graphConnection, onedrive.ReadFileTextInput{ItemID: fileID})
		require.NoError(t, err)
		require.Equal(t, onedrive.ReadFileTextBranchUnsupportedContent, result.Branch)
		require.Empty(t, result.Value.Text)
	}
}

func TestReadFileTextHandlesUnusableRedirectsAndExpiredLinks(t *testing.T) {
	for _, test := range []struct {
		name       string
		location   string
		wantBranch sdkgo.BranchID
		wantRetry  bool
	}{
		{name: "plain HTTP to a public host", location: "http://contoso.sharepoint.com/download?tempauth=x", wantBranch: onedrive.ReadFileTextBranchInvalidResponse},
		{name: "relative location", location: "/download?tempauth=x", wantBranch: onedrive.ReadFileTextBranchInvalidResponse},
		{name: "user information", location: "https://user:pass@contoso.sharepoint.com/download", wantBranch: onedrive.ReadFileTextBranchInvalidResponse},
		{name: "expired pre-authenticated link", location: "{server}/download/01FAKEITEM0001?tempauth=expired", wantRetry: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			graph := newGraph(t)
			fileID := graph.AddItem(graphfake.Item{Name: "notes.txt", DriveID: myDriveID, ParentID: "root", MimeType: "text/plain", Content: []byte("notes")})
			location := strings.ReplaceAll(test.location, "{server}", graph.URL)
			graph.Intercept(func(request graphfake.Request) *graphfake.Response {
				if strings.HasSuffix(request.Path, "/content") {
					return &graphfake.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": {location}}, Body: []byte{}}
				}
				return nil
			})
			client := newGraphClient(t, graph.URL)
			result, err := sdkgo.RunQuery(newDexContext("read-redirect"), client.ReadFileText(), graphConnection, onedrive.ReadFileTextInput{ItemID: fileID})
			if test.wantRetry {
				require.Error(t, err)
				require.Contains(t, err.Error(), "file download link expired")
				require.NotContains(t, err.Error(), "tempauth")
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.wantBranch, result.Branch)
			require.Equal(t, 2, len(graph.Requests()), "an unusable redirect is never followed")
			requireSecretFree(t, result)
			require.NotContains(t, result.Failure.Message, "sharepoint")
		})
	}
}

func TestReadFileTextSelectsNotFoundAndRetriesThrottledContent(t *testing.T) {
	graph := newGraph(t)
	client := newGraphClient(t, graph.URL)
	missing, err := sdkgo.RunQuery(newDexContext("read-missing"), client.ReadFileText(), graphConnection, onedrive.ReadFileTextInput{ItemID: "01MISSING"})
	require.NoError(t, err)
	require.Equal(t, onedrive.ReadFileTextBranchNotFound, missing.Branch)

	fileID := graph.AddItem(graphfake.Item{Name: "notes.txt", DriveID: myDriveID, ParentID: "root", MimeType: "text/plain", Content: []byte("notes")})
	graph.Intercept(func(request graphfake.Request) *graphfake.Response {
		if strings.HasSuffix(request.Path, "/content") {
			return &graphfake.Response{StatusCode: http.StatusTooManyRequests, Code: "activityLimitReached"}
		}
		return nil
	})
	_, err = sdkgo.RunQuery(newDexContext("read-throttled"), client.ReadFileText(), graphConnection, onedrive.ReadFileTextInput{ItemID: fileID})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "GRAPH-MESSAGE-SENTINEL")

	for _, input := range []onedrive.ReadFileTextInput{{}, {ItemID: "root"}, {ItemID: "x y"}} {
		result, err := sdkgo.RunQuery(newDexContext("read-invalid"), client.ReadFileText(), graphConnection, input)
		require.NoError(t, err)
		require.Equal(t, onedrive.ReadFileTextBranchDefect, result.Branch)
	}
}
