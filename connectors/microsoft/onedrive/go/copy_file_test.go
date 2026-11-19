// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package onedrive_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/onedrive/go"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/onedrive/go/internal/graphfake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestCopyFileRetriesAmbiguousProviderResponses(t *testing.T) {
	for _, response := range []struct {
		name   string
		status int
		code   string
	}{
		{"conflict", http.StatusConflict, "nameAlreadyExists"},
		{"unlisted status", http.StatusMethodNotAllowed, "methodNotAllowed"},
		{"empty refusal", http.StatusBadRequest, ""},
		{"unavailable", http.StatusServiceUnavailable, "serviceUnavailable"},
	} {
		t.Run(response.name, func(t *testing.T) {
			graph := newGraph(t)
			itemID := graph.AddItem(graphfake.Item{Name: "source.txt", DriveID: teamDriveID, ParentID: "root", MimeType: "text/plain", Content: []byte("x")})
			graph.Intercept(func(request graphfake.Request) *graphfake.Response {
				if request.Method == http.MethodPost && strings.HasSuffix(request.Path, "/copy") {
					return &graphfake.Response{StatusCode: response.status, Code: response.code}
				}
				return nil
			})
			_, err := sdkgo.RunMutation(newDexContext("copy-ambiguous"), newGraphClient(t, graph.URL).CopyFile(), graphConnection, onedrive.CopyFileInput{
				DriveID: teamDriveID, ItemID: itemID, DestinationDriveID: teamDriveID, DestinationFolderID: "root", Name: "copy.txt",
			})
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
		})
	}
}

func TestCopyFileAuthorizationChangesAfterPossibleSend(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		maxSends  int32
		uncertain bool
	}{
		{name: "send remains available", maxSends: 3},
		{name: "send cap reached", maxSends: 1, uncertain: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			graph := newGraph(t)
			itemID := graph.AddItem(graphfake.Item{Name: "source.txt", DriveID: teamDriveID, ParentID: "root", MimeType: "text/plain", Content: []byte("x")})
			graph.Intercept(func(request graphfake.Request) *graphfake.Response {
				if request.Method == http.MethodPost && strings.HasSuffix(request.Path, "/copy") {
					return &graphfake.Response{StatusCode: http.StatusInternalServerError, Code: "internalServerError"}
				}
				return nil
			})
			input := onedrive.CopyFileInput{DriveID: teamDriveID, ItemID: itemID, DestinationFolderID: "root", Name: "copy.txt"}
			context := newDexContext("copy-authorization-change")
			_, err := sdkgo.RunMutation(context, newGraphClientWithMethod(t, graph.URL, onedrive.MicrosoftOAuthAuthMethodID).CopyFile(), graphConnection, input, sdkgo.WithMaxSends(testCase.maxSends))
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
			require.NotEmpty(t, context.recordedHeartbeat)

			requestCount := len(graph.Requests())
			context.previousHeartbeat = append([]byte(nil), context.recordedHeartbeat...)
			context.attempt++
			result, err := sdkgo.RunMutation(context, newGraphClientWithMethod(t, graph.URL, onedrive.MicrosoftAppOnlyAuthMethodID).CopyFile(), graphConnection, input, sdkgo.WithMaxSends(testCase.maxSends))
			if testCase.uncertain {
				require.NoError(t, err)
				require.Equal(t, onedrive.CopyFileBranchUncertain, result.Branch)
			} else {
				require.ErrorAs(t, err, &retry)
			}
			require.NotEqual(t, onedrive.CopyFileBranchDefect, result.Branch)
			require.Len(t, graph.Requests(), requestCount, "app-only authorization cannot read the original destination or resend")
		})
	}
}
