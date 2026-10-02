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
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/onedrive/internal/graphfake"
	"github.com/superdurable/dex/sdk-go/dex"
)

// slowResponseDelay outlasts the seven-second async local phase, so Dex dispatches the Step again.
const slowResponseDelay = 9 * time.Second

// delayFirstMatching holds only the first matching response, after the fake has applied the write.
func delayFirstMatching(graph *graphFixture, matches func(graphfake.Request) bool) {
	var delayed atomic.Bool
	graph.DelayResponses(func(request graphfake.Request) time.Duration {
		if matches(request) && delayed.CompareAndSwap(false, true) {
			return slowResponseDelay
		}
		return 0
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
			delayFirstMatching(graph, isCopyUpload)
			harness := newTextCopyHarness(t, graph)

			result := harness.run(t, "duplicate-upload", Input{
				SourceName: "Ops Policy.txt", CopyFolderName: "Dex copies", CopyName: "slow.txt", ShouldReplaceExisting: test.shouldReplaceExisting,
			})

			require.Equal(t, dex.FlowCompleted, result.status)
			require.Equal(t, StatusCopied, result.outcome.Status)
			puts := graph.CountRequests(http.MethodPut, func(path string) bool { return strings.HasSuffix(path, "/slow.txt:/content") })
			require.Equal(t, 2, puts, "Dex dispatched the slow upload Step a second time")
			copies := graph.ItemsNamed(destinationDrive, result.outcome.CopyFolder.ID, "slow.txt")
			require.Len(t, copies, 1)
			require.Equal(t, sourceText, string(copies[0].Content))
			t.Logf("duplicate upload dispatch: puts=%d isCopyExistingIdentical=%t", puts, result.outcome.IsCopyExistingIdentical)
		})
	}
}

func TestTextCopyDuplicateFolderDispatchCreatesOneFolderWithRealDex(t *testing.T) {
	graph := newGraphFixture(t)
	delayFirstMatching(graph, func(request graphfake.Request) bool {
		return request.Method == http.MethodPost && strings.HasSuffix(request.Path, "/children")
	})
	harness := newTextCopyHarness(t, graph)

	result := harness.run(t, "duplicate-folder", Input{SourceName: "Ops Policy.txt", CopyFolderName: "Slow folder", CopyName: "copy.txt"})

	require.Equal(t, dex.FlowCompleted, result.status)
	require.Equal(t, StatusCopied, result.outcome.Status)
	posts := graph.CountRequests(http.MethodPost, func(path string) bool { return strings.HasSuffix(path, "/children") })
	require.Equal(t, 2, posts, "Dex dispatched the slow folder Step a second time")
	require.Len(t, graph.ItemsNamed(destinationDrive, graph.reportsFolderID, "Slow folder"), 1)
	t.Logf("duplicate folder dispatch: posts=%d isCopyFolderExisting=%t", posts, result.outcome.IsCopyFolderExisting)
}
