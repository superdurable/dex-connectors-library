// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package onedrive_test

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/onedrive"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/onedrive/internal/graphfake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestSearchFilesReadsAnExactNameInAFolderByPath(t *testing.T) {
	graph := newGraph(t)
	folderID := graph.AddItem(graphfake.Item{Name: "Policies", DriveID: teamDriveID, ParentID: "root", IsFolder: true})
	fileID := graph.AddItem(graphfake.Item{Name: "Ops Policy.txt", DriveID: teamDriveID, ParentID: folderID, MimeType: "text/plain", Content: []byte("policy")})
	graph.AddItem(graphfake.Item{Name: "Ops Policy.txt", DriveID: teamDriveID, ParentID: "root", MimeType: "text/plain", Content: []byte("other folder")})
	client := newGraphClient(t, graph.URL)

	result, err := sdkgo.RunQuery(newDexContext("search-path"), client.SearchFiles(), graphConnection, onedrive.SearchFilesInput{
		DriveID: teamDriveID, ParentFolderID: folderID, Name: "ops policy.TXT",
	})
	require.NoError(t, err)
	require.Equal(t, onedrive.SearchFilesBranchFound, result.Branch)
	require.Len(t, result.Value.Files, 1)
	require.Equal(t, fileID, result.Value.Files[0].ID)
	require.Equal(t, "Ops Policy.txt", result.Value.Files[0].Name)
	require.Equal(t, teamDriveID, result.Value.Files[0].DriveID)
	require.False(t, result.Value.IsIndexedSearch)
	requests := graph.Requests()
	require.Len(t, requests, 1)
	require.Equal(t, "/v1.0/drives/"+teamDriveID+"/items/"+folderID+":/ops policy.TXT", requests[0].Path)
	require.Contains(t, requests[0].Query.Get("$select"), "parentReference")
	requireSecretFree(t, result)

	missing, err := sdkgo.RunQuery(newDexContext("search-path-missing"), client.SearchFiles(), graphConnection, onedrive.SearchFilesInput{
		DriveID: teamDriveID, ParentFolderID: folderID, Name: "Ops Polcy.txt",
	})
	require.NoError(t, err)
	require.Equal(t, onedrive.SearchFilesBranchNotFound, missing.Branch)
	require.Empty(t, missing.Value.Files)
	require.Equal(t, sdkgo.FailureNotFound, missing.Failure.Kind)
}

func TestSearchFilesListsAFolderPageByPageAndFiltersEachPage(t *testing.T) {
	graph := newGraph(t)
	folderID := graph.AddItem(graphfake.Item{Name: "Reports", DriveID: myDriveID, ParentID: "root", IsFolder: true})
	for _, name := range []string{"a-report.csv", "b-notes.txt", "c-report.csv", "d-report.txt"} {
		mimeType := "text/csv"
		if strings.HasSuffix(name, ".txt") {
			mimeType = "text/plain"
		}
		graph.AddItem(graphfake.Item{Name: name, DriveID: myDriveID, ParentID: folderID, MimeType: mimeType, Content: []byte(name)})
	}
	client := newGraphClient(t, graph.URL)
	input := onedrive.SearchFilesInput{ParentFolderID: folderID, Name: "REPORT", NameMatch: onedrive.NameMatchContains, MimeType: "text/csv", PageSize: 2}

	first, err := sdkgo.RunQuery(newDexContext("search-children-1"), client.SearchFiles(), graphConnection, input)
	require.NoError(t, err)
	require.Equal(t, onedrive.SearchFilesBranchFound, first.Branch)
	require.Equal(t, []string{"a-report.csv"}, summaryNames(first.Value.Files))
	require.True(t, strings.HasPrefix(first.Value.NextPageToken, graph.URL+"/v1.0/me/drive/items/"+folderID+"/children?"))
	require.Equal(t, "/v1.0/me/drive/items/"+folderID+"/children", graph.Requests()[0].Path, "a blank drive ID addresses the signed-in user's OneDrive")
	require.Equal(t, "2", graph.Requests()[0].Query.Get("$top"))

	input.PageToken = first.Value.NextPageToken
	second, err := sdkgo.RunQuery(newDexContext("search-children-2"), client.SearchFiles(), graphConnection, input)
	require.NoError(t, err)
	require.Equal(t, onedrive.SearchFilesBranchFound, second.Branch)
	require.Equal(t, []string{"c-report.csv"}, summaryNames(second.Value.Files), "d-report.txt is not CSV")
	require.Empty(t, second.Value.NextPageToken)
	require.Equal(t, "2", graph.Requests()[1].Query.Get("$skiptoken"))

	everything, err := sdkgo.RunQuery(newDexContext("search-children-all"), client.SearchFiles(), graphConnection, onedrive.SearchFilesInput{ParentFolderID: folderID})
	require.NoError(t, err)
	require.Equal(t, []string{"a-report.csv", "b-notes.txt", "c-report.csv", "d-report.txt"}, summaryNames(everything.Value.Files))
}

func TestSearchFilesRunsDriveSearchWithAnEscapedODataLiteral(t *testing.T) {
	graph := newGraph(t)
	graph.AddItem(graphfake.Item{Name: "O'Reilly notes.md", DriveID: teamDriveID, ParentID: "root", MimeType: "application/octet-stream", Content: []byte("x")})
	graph.AddItem(graphfake.Item{Name: "O'Reilly notes.md.bak", DriveID: teamDriveID, ParentID: "root", MimeType: "application/octet-stream", Content: []byte("y")})
	client := newGraphClient(t, graph.URL)

	result, err := sdkgo.RunQuery(newDexContext("search-drive"), client.SearchFiles(), graphConnection, onedrive.SearchFilesInput{
		DriveID: teamDriveID, Name: "o'reilly notes.md",
	})
	require.NoError(t, err)
	require.Equal(t, onedrive.SearchFilesBranchFound, result.Branch)
	require.Equal(t, []string{"O'Reilly notes.md"}, summaryNames(result.Value.Files), "exact matching drops the .bak name the index also returned")
	require.True(t, result.Value.IsIndexedSearch)
	require.Equal(t, "/v1.0/drives/"+teamDriveID+"/root/search(q='o''reilly notes.md')", graph.Requests()[0].Path)
}

func TestSearchFilesRejectsInvalidInputWithoutAProviderRequest(t *testing.T) {
	graph := newGraph(t)
	client := newGraphClient(t, graph.URL)
	for name, input := range map[string]onedrive.SearchFilesInput{
		"drive-wide search without a name": {DriveID: teamDriveID},
		"reserved character":               {DriveID: teamDriveID, Name: "a:b"},
		"whitespace name":                  {DriveID: teamDriveID, Name: "  "},
		"match without name":               {DriveID: teamDriveID, ParentFolderID: "root", NameMatch: onedrive.NameMatchExact},
		"unknown match":                    {DriveID: teamDriveID, Name: "a", NameMatch: "fuzzy"},
		"drive ID with a slash":            {DriveID: "b!x/../y", Name: "a"},
		"folder ID with a space":           {ParentFolderID: "a b", Name: "a"},
		"media type with parameters":       {Name: "a", MimeType: "text/plain; charset=utf-8"},
		"page size above 200":              {Name: "a", PageSize: 201},
		"page token on another host":       {Name: "a", PageToken: "https://graph.example.com/v1.0/me/drive/root/children?$skiptoken=1"},
		"page token outside the API":       {Name: "a", PageToken: graph.URL + "/download/01FAKEITEM0001?tempauth=x"},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := sdkgo.RunQuery(newDexContext("search-invalid"), client.SearchFiles(), graphConnection, input)
			require.NoError(t, err)
			require.Equal(t, onedrive.SearchFilesBranchDefect, result.Branch)
			require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
		})
	}
	require.Empty(t, graph.Requests())
}

func TestSearchFilesNeedsADriveIDForAppOnlyConnections(t *testing.T) {
	graph := newGraph(t)
	client := newGraphClientWithMethod(t, graph.URL, onedrive.MicrosoftAppOnlyAuthMethodID)
	result, err := sdkgo.RunQuery(newDexContext("search-app-only"), client.SearchFiles(), graphConnection, onedrive.SearchFilesInput{Name: "a"})
	require.NoError(t, err)
	require.Equal(t, onedrive.SearchFilesBranchDefect, result.Branch)
	require.Contains(t, result.Failure.Message, "app-only")
	require.Empty(t, graph.Requests())
}

func TestSearchFilesClassifiesGraphFailuresWithoutProviderText(t *testing.T) {
	for _, test := range []struct {
		name          string
		response      graphfake.Response
		wantBranch    sdkgo.BranchID
		wantKind      sdkgo.FailureKind
		wantRetryWait time.Duration
	}{
		{name: "throttled", response: graphfake.Response{StatusCode: http.StatusTooManyRequests, Code: "TooManyRequests", Header: http.Header{"Retry-After": {"7"}}}, wantKind: sdkgo.FailureRateLimit, wantRetryWait: 7 * time.Second},
		{name: "unavailable", response: graphfake.Response{StatusCode: http.StatusServiceUnavailable, Code: "serviceNotAvailable"}, wantKind: sdkgo.FailureRateLimit},
		{name: "Sites.Selected search", response: graphfake.Response{StatusCode: http.StatusForbidden, Code: "accessDenied"}, wantBranch: onedrive.SearchFilesBranchProviderRejected, wantKind: sdkgo.FailureAuthorization},
		{name: "missing drive", response: graphfake.Response{StatusCode: http.StatusNotFound, Code: "itemNotFound"}, wantBranch: onedrive.SearchFilesBranchProviderRejected, wantKind: sdkgo.FailureNotFound},
		{name: "foreign next link", response: graphfake.Response{StatusCode: http.StatusOK, Body: []byte(`{"value":[],"@odata.nextLink":"https://evil.example/v1.0/x"}`)}, wantBranch: onedrive.SearchFilesBranchInvalidResponse, wantKind: sdkgo.FailureProtocol},
		{name: "invalid item", response: graphfake.Response{StatusCode: http.StatusOK, Body: []byte(`{"value":[{"id":"bad id","name":"x"}]}`)}, wantBranch: onedrive.SearchFilesBranchInvalidResponse, wantKind: sdkgo.FailureProtocol},
	} {
		t.Run(test.name, func(t *testing.T) {
			graph := newGraph(t)
			response := test.response
			graph.Intercept(func(graphfake.Request) *graphfake.Response { return &response })
			client := newGraphClient(t, graph.URL)
			result, err := sdkgo.RunQuery(newDexContext("search-failure"), client.SearchFiles(), graphConnection, onedrive.SearchFilesInput{DriveID: teamDriveID, Name: "a"})
			if test.wantBranch == "" {
				var retry *dex.RetryAfterError
				require.Error(t, err)
				if test.wantRetryWait > 0 {
					require.True(t, errors.As(err, &retry))
					require.Equal(t, test.wantRetryWait, retry.After)
				}
				require.NotContains(t, err.Error(), "GRAPH-MESSAGE-SENTINEL")
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.wantBranch, result.Branch)
			require.Equal(t, test.wantKind, result.Failure.Kind)
			requireSecretFree(t, result)
		})
	}
}

func TestSearchFilesSelectsInvalidResponseForAnOversizedPage(t *testing.T) {
	graph := newGraph(t)
	for index := 0; index < 30; index++ {
		graph.AddItem(graphfake.Item{Name: strings.Repeat("n", 100) + string(rune('a'+index%26)), DriveID: teamDriveID, ParentID: "root", MimeType: "text/plain"})
	}
	client := newGraphClient(t, graph.URL, onedrive.Config{MaxResponseBytes: 2048})
	result, err := sdkgo.RunQuery(newDexContext("search-oversized"), client.SearchFiles(), graphConnection, onedrive.SearchFilesInput{DriveID: teamDriveID, ParentFolderID: "root"})
	require.NoError(t, err)
	require.Equal(t, onedrive.SearchFilesBranchInvalidResponse, result.Branch)
	require.Equal(t, sdkgo.FailureResponseTooLarge, result.Failure.Kind)
}

func summaryNames(files []onedrive.FileSummary) []string {
	names := make([]string, 0, len(files))
	for _, file := range files {
		names = append(names, file.Name)
	}
	return names
}
