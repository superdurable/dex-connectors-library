// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package eventtrack

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/mixpanel/go"
)

func TestPrepareRequestFixesTimeAndMapsToImport(t *testing.T) {
	moment := time.Unix(1793232000, 0)
	prepared, err := PrepareRequest(Request{Event: " Signed Up ", DistinctID: " member-1 "}, moment)
	require.NoError(t, err)
	require.Equal(t, Request{Event: "Signed Up", DistinctID: "member-1", Time: 1793232000}, prepared)
	require.Equal(t, mixpanel.TrackEventInput{Event: "Signed Up", DistinctID: "member-1", Time: 1793232000}, MapToTrackEventInput(prepared))
}
