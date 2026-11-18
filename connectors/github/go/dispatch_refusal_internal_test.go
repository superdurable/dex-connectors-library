// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package github

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

func newUndecidedFence(t *testing.T, step string) (sdkgo.Call, *singledispatch.Fence) {
	t.Helper()
	call := sdkgo.Call{
		Context:    singledispatchtest.NewContext("issues-flow", step),
		ID:         "0a1b2c3d-0000-5000-8000-000000000001",
		Connection: sdkgo.ConnectionRef{Provider: "github", Name: "github"},
	}
	// Without Decide, the Fence refuses Dispatch with ErrDispatchNotAllowed.
	fence, err := singledispatch.NewFence(call, 30*time.Second)
	require.NoError(t, err)
	return call, fence
}

func TestCreateIssueRefusedDispatchSelectsUncertainOnlyAfterAPossibleSend(t *testing.T) {
	call, fence := newUndecidedFence(t, "issue-refused-first")
	writer := &issueWriter{call: call, marker: issueCallMarker(call.ID)}
	require.Equal(t, writer.branch(CreateIssueBranchDefect, sdkgo.FailureLocalDefect, "GitHub dispatch was not allowed", sdkgo.Receipt{}),
		writer.dispatch(fence, createIssueRequestBody{Title: "Bug"}, 1, false))

	call, fence = newUndecidedFence(t, "issue-refused-resend")
	writer = &issueWriter{call: call, marker: issueCallMarker(call.ID)}
	require.Equal(t, writer.uncertain(sdkgo.FailureLocalDefect, "GitHub dispatch was not allowed after an earlier possible send", sdkgo.Receipt{}),
		writer.dispatch(fence, createIssueRequestBody{Title: "Bug"}, 2, true), "an earlier send may have created the issue")
}

func TestCreateIssueCommentRefusedDispatchSelectsUncertainOnlyAfterAPossibleSend(t *testing.T) {
	call, fence := newUndecidedFence(t, "comment-refused-first")
	writer := &commentWriter{call: call}
	require.Equal(t, writer.branch(CreateIssueCommentBranchDefect, sdkgo.FailureLocalDefect, "GitHub dispatch was not allowed", sdkgo.Receipt{}),
		writer.dispatch(fence, createIssueCommentRequestBody{Body: "Thanks"}, 1, false))

	call, fence = newUndecidedFence(t, "comment-refused-resend")
	writer = &commentWriter{call: call}
	require.Equal(t, writer.uncertain(sdkgo.FailureLocalDefect, "GitHub dispatch was not allowed after an earlier possible send", sdkgo.Receipt{}),
		writer.dispatch(fence, createIssueCommentRequestBody{Body: "Thanks"}, 2, true), "an earlier send may have created the comment")
}
