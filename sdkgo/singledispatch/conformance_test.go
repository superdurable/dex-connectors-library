// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package singledispatch

import (
	"testing"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/internal/conformancefixture"
)

func TestConformanceMarkers(t *testing.T) {
	callID := sdkgo.CallID(conformancefixture.ExampleCallID)
	conformancefixture.Check(t, "single-dispatch-markers.json", []Marker{
		{CallID: callID, Attempt: 1, State: StateNotSent},
		{CallID: callID, Attempt: 2, State: StateClaimed},
		{CallID: callID, Attempt: 3, State: StateCanResend},
		{CallID: callID, Attempt: 4, State: StateClaimed, ProviderID: "object-42"},
		{CallID: callID, Attempt: 5, State: StateClaimed, SendCount: 2},
		{CallID: callID, Attempt: 6, State: StateNotSent, SendCount: 2},
		{CallID: callID, Attempt: 7, State: StateCanResend, SendCount: 2},
		{CallID: callID, Attempt: 8, State: StateClaimed, ProviderID: "object-43", SendCount: 3},
	})
}
