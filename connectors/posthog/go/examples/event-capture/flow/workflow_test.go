// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package eventcapture

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/posthog/go"
)

func TestPrepareRequestFixesTimeAndMapsToCapture(t *testing.T) {
	moment := time.Unix(1793232000, 0)
	prepared, err := PrepareRequest(Request{Event: " Signed Up ", DistinctID: " member-1 "}, moment)
	require.NoError(t, err)
	require.Equal(t, Request{Event: "Signed Up", DistinctID: "member-1", Timestamp: "2026-10-29T00:00:00Z"}, prepared)
	require.Equal(t, posthog.CaptureEventInput{Event: "Signed Up", DistinctID: "member-1", Timestamp: prepared.Timestamp}, MapToCaptureEventInput(prepared))
}
