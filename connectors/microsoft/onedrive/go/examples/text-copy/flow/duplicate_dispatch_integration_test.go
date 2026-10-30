//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package textcopy

import (
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/onedrive/go/internal/graphfake"
	"github.com/superdurable/dex/sdk-go/dex"
)

// holdFirstMatching loses the first matching response after the fake applied the write.
func holdFirstMatching(graph *graphFixture, matches func(graphfake.Request) bool) {
	var held atomic.Bool
	graph.HoldResponses(func(request graphfake.Request) bool {
		return matches(request) && held.CompareAndSwap(false, true)
	})
}

func TestTextCopyDuplicateUploadDispatchWritesOneCopyWithRealDex(t *testing.T) {
	for _, test := range []struct {
		name                  string
		shouldReplaceExisting bool
	}{
		{name: "fail conflict behavior"},
		{name: "replace conflict behavior", shouldReplaceExisting: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			graph := newGraphFixture(t)
			holdFirstMatching(graph, isCopyUpload)
			harness := newTextCopyHarness(t, graph)

			flowID := harness.start(t, "duplicate-upload", Input{
				SourceName: "Ops Policy.txt", CopyFolderName: "Dex copies", CopyName: "slow.txt", ShouldReplaceExisting: test.shouldReplaceExisting,
			})
			require.Eventually(t, func() bool {
				folders := graph.ItemsNamed(destinationDrive, graph.reportsFolderID, "Dex copies")
				return len(folders) == 1 && len(graph.ItemsNamed(destinationDrive, folders[0].ID, "slow.txt")) == 1
			}, 30*time.Second, 10*time.Millisecond)
			harness.replaceWorker(t)
			result := harness.wait(t, flowID)

			require.Equal(t, dex.FlowCompleted, result.status)
			require.Equal(t, StatusCopied, result.outcome.Status)
			puts := graph.CountRequests(http.MethodPut, func(path string) bool { return strings.HasSuffix(path, "/slow.txt:/content") })
			require.Equal(t, 2, puts, "the replacement Worker repeats the upload once")
			copies := graph.ItemsNamed(destinationDrive, result.outcome.CopyFolder.ID, "slow.txt")
			require.Len(t, copies, 1)
			require.Equal(t, sourceText, string(copies[0].Content))
			t.Logf("duplicate upload dispatch: puts=%d isCopyExistingIdentical=%t", puts, result.outcome.IsCopyExistingIdentical)
		})
	}
}

func TestTextCopyDuplicateFolderDispatchCreatesOneFolderWithRealDex(t *testing.T) {
	graph := newGraphFixture(t)
	holdFirstMatching(graph, func(request graphfake.Request) bool {
		return request.Method == http.MethodPost && strings.HasSuffix(request.Path, "/children")
	})
	harness := newTextCopyHarness(t, graph)

	flowID := harness.start(t, "duplicate-folder", Input{SourceName: "Ops Policy.txt", CopyFolderName: "Slow folder", CopyName: "copy.txt"})
	require.Eventually(t, func() bool {
		return len(graph.ItemsNamed(destinationDrive, graph.reportsFolderID, "Slow folder")) == 1
	}, 30*time.Second, 10*time.Millisecond)
	harness.replaceWorker(t)
	result := harness.wait(t, flowID)

	require.Equal(t, dex.FlowCompleted, result.status)
	require.Equal(t, StatusCopied, result.outcome.Status)
	posts := graph.CountRequests(http.MethodPost, func(path string) bool { return strings.HasSuffix(path, "/children") })
	require.Equal(t, 2, posts, "the replacement Worker repeats the folder create once")
	require.Len(t, graph.ItemsNamed(destinationDrive, graph.reportsFolderID, "Slow folder"), 1)
	t.Logf("duplicate folder dispatch: posts=%d isCopyFolderExisting=%t", posts, result.outcome.IsCopyFolderExisting)
}
