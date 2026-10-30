// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package apirequest

import (
	"testing"

	oauth2http "github.com/superdurable/dex-connectors-library/connectors/superdurable/oauth2-http/go"
	"github.com/superdurable/dex/sdk-go/dex"
)

// TestRegisteredTypesMatchTheFlowDefinitionGraph guards Dex Web Start Flow, which starts the graph's
// unqualified Flow and start Step names.
func TestRegisteredTypesMatchTheFlowDefinitionGraph(t *testing.T) {
	if flowType := dex.GetFinalFlowType(&Flow{}); flowType != "OAuth2HTTPRequest" {
		t.Fatalf("Flow type = %q", flowType)
	}
	for got, want := range map[string]string{
		dex.GetFinalStepType[Input](startRequest{}):                          "startRequest",
		dex.GetFinalStepType[oauth2http.SendRequestResult](recordResponse{}): "recordResponse",
	} {
		if got != want {
			t.Fatalf("Step type = %q, want %q", got, want)
		}
	}
}
