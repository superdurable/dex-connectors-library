// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package onedrive_test

import (
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/onedrive"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/onedrive/internal/graphfake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func isUpload(request graphfake.Request) bool {
	return request.Method == http.MethodPut && strings.HasSuffix(request.Path, ":/content")
}

// A stored file whose response was lost is retried, and the repeated PUT converges on it.
func TestUploadFileRetriesALostResponseAndConvergesOnOneFile(t *testing.T) {
	for _, test := range []struct {
		name     string
		behavior onedrive.ConflictBehavior
		lost     graphfake.Response
	}{
		{name: "fail after a dropped connection", behavior: onedrive.ConflictBehaviorFail, lost: graphfake.Response{ShouldDropConnection: true}},
		{name: "fail after a 500", behavior: onedrive.ConflictBehaviorFail, lost: graphfake.Response{StatusCode: http.StatusInternalServerError, Code: "generalException"}},
		{name: "replace after a 504", behavior: onedrive.ConflictBehaviorReplace, lost: graphfake.Response{StatusCode: http.StatusGatewayTimeout, Code: "UnknownError"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			graph := newGraph(t)
			var uploads atomic.Int32
			lost := test.lost
			graph.InterceptAfterApply(func(request graphfake.Request) *graphfake.Response {
				if isUpload(request) && uploads.Add(1) == 1 {
					return &lost
				}
				return nil
			})
			client := newGraphClient(t, graph.URL)
			ctx := newDexContext("upload-lost-response")
			input := uploadInput("root", test.behavior, "report\n")

			_, err := sdkgo.RunMutation(ctx, client.UploadFile(), graphConnection, input)
			require.Error(t, err, "an unknown outcome is retried, because a PUT by path cannot duplicate")
			recovered, err := sdkgo.RunMutation(ctx, client.UploadFile(), graphConnection, input)
			require.NoError(t, err)
			require.Equal(t, onedrive.UploadFileBranchUploaded, recovered.Branch)
			require.Equal(t, test.behavior == onedrive.ConflictBehaviorFail, recovered.Value.IsExistingFileIdentical)
			require.Len(t, graph.ItemsNamed(teamDriveID, "root", input.Name), 1)
			require.Equal(t, int32(2), uploads.Load())
		})
	}
}

func TestUploadFileRetriesWhileTheExistingFileHashIsPending(t *testing.T) {
	graph := newGraph(t)
	graph.AddItem(graphfake.Item{Name: "Jan-2026 reconciliation.csv", DriveID: teamDriveID, ParentID: "root", MimeType: "text/csv", Content: []byte("v1\n"), IsHashPending: true})
	client := newGraphClient(t, graph.URL)
	_, err := sdkgo.RunMutation(newDexContext("upload-hash-pending"), client.UploadFile(), graphConnection, uploadInput("root", onedrive.ConflictBehaviorFail, "v1\n"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "content hash")
}

func TestUploadFileClassifiesRejectionsAndThrottling(t *testing.T) {
	for _, test := range []struct {
		name       string
		response   graphfake.Response
		wantBranch sdkgo.BranchID
		wantKind   sdkgo.FailureKind
	}{
		{name: "throttled", response: graphfake.Response{StatusCode: http.StatusTooManyRequests, Code: "activityLimitReached", Header: http.Header{"Retry-After": {"3"}}}},
		{name: "locked with other content", response: graphfake.Response{StatusCode: http.StatusLocked, Code: "resourceLocked"}, wantBranch: onedrive.UploadFileBranchProviderRejected, wantKind: sdkgo.FailureConflict},
		{name: "denied", response: graphfake.Response{StatusCode: http.StatusForbidden, Code: "accessDenied"}, wantBranch: onedrive.UploadFileBranchProviderRejected, wantKind: sdkgo.FailureAuthorization},
		{name: "quota", response: graphfake.Response{StatusCode: http.StatusInsufficientStorage, Code: "quotaLimitReached"}, wantBranch: onedrive.UploadFileBranchProviderRejected, wantKind: sdkgo.FailureQuotaExhausted},
		{name: "blocked type", response: graphfake.Response{StatusCode: http.StatusBadRequest, Code: "invalidRequest"}, wantBranch: onedrive.UploadFileBranchProviderRejected, wantKind: sdkgo.FailureProviderRejection},
	} {
		t.Run(test.name, func(t *testing.T) {
			graph := newGraph(t)
			graph.AddItem(graphfake.Item{Name: "Jan-2026 reconciliation.csv", DriveID: teamDriveID, ParentID: "root", MimeType: "text/csv", Content: []byte("other\n")})
			response := test.response
			graph.Intercept(func(request graphfake.Request) *graphfake.Response {
				if isUpload(request) {
					return &response
				}
				return nil
			})
			client := newGraphClient(t, graph.URL)
			result, err := sdkgo.RunMutation(newDexContext("upload-rejected"), client.UploadFile(), graphConnection, uploadInput("root", onedrive.ConflictBehaviorReplace, "v1\n"))
			if test.wantBranch == "" {
				require.Error(t, err)
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

func TestUploadFileMissingParentIsRejectedWithoutRetry(t *testing.T) {
	graph := newGraph(t)
	client := newGraphClient(t, graph.URL)
	result, err := sdkgo.RunMutation(newDexContext("upload-missing-parent"), client.UploadFile(), graphConnection, uploadInput("01NOFOLDER", onedrive.ConflictBehaviorFail, "x"))
	require.NoError(t, err)
	require.Equal(t, onedrive.UploadFileBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureNotFound, result.Failure.Kind)
	require.Equal(t, 1, graph.CountRequests(http.MethodPut, anyPath))
}

func TestUploadFileRejectsInvalidInputWithoutAProviderRequest(t *testing.T) {
	graph := newGraph(t)
	client := newGraphClient(t, graph.URL, onedrive.Config{MaxUploadBytes: 8})
	valid := uploadInput("root", onedrive.ConflictBehaviorFail, "ok")
	for name, change := range map[string]func(*onedrive.UploadFileInput){
		"rename behavior":      func(input *onedrive.UploadFileInput) { input.ConflictBehavior = "rename" },
		"blank behavior":       func(input *onedrive.UploadFileInput) { input.ConflictBehavior = "" },
		"reserved character":   func(input *onedrive.UploadFileInput) { input.Name = "a:b.txt" },
		"leading tilde":        func(input *onedrive.UploadFileInput) { input.Name = "~$draft.txt" },
		"trailing space":       func(input *onedrive.UploadFileInput) { input.Name = "a.txt " },
		"blank name":           func(input *onedrive.UploadFileInput) { input.Name = "" },
		"media type parameter": func(input *onedrive.UploadFileInput) { input.MimeType = "text/plain; charset=utf-8" },
		"both contents":        func(input *onedrive.UploadFileInput) { input.ByteContent = []byte("x") },
		"over the limit":       func(input *onedrive.UploadFileInput) { input.TextContent = "123456789" },
		"invalid parent":       func(input *onedrive.UploadFileInput) { input.ParentFolderID = "../x" },
	} {
		input := valid
		change(&input)
		result, err := sdkgo.RunMutation(newDexContext("upload-invalid"), client.UploadFile(), graphConnection, input)
		require.NoError(t, err, name)
		require.Equal(t, onedrive.UploadFileBranchDefect, result.Branch, name)
	}
	require.Empty(t, graph.Requests())
}
