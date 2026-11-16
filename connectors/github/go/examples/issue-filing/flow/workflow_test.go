// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package issuefiling

import (
	"testing"

	"github.com/superdurable/dex-connectors-library/connectors/github/go"
	"github.com/superdurable/dex/sdk-go/dex"
)

// TestRegisteredTypesMatchTheFlowDefinitionGraph guards Dex Web Start Flow, which starts the graph's
// unqualified Flow and start Step names.
func TestRegisteredTypesMatchTheFlowDefinitionGraph(t *testing.T) {
	if flowType := dex.GetFinalFlowType(&Flow{}); flowType != "GitHubIssueFiling" {
		t.Fatalf("Flow type = %q", flowType)
	}
	for got, want := range map[string]string{
		dex.GetFinalStepType[Input](startIssueFiling{}):                       "startIssueFiling",
		dex.GetFinalStepType[github.ListIssuesResult](recordOpenIssues{}):     "recordOpenIssues",
		dex.GetFinalStepType[github.CreateIssueResult](completeIssueFiling{}): "completeIssueFiling",
		dex.GetFinalStepType[github.SetIssueStateResult](recordClosedIssue{}): "recordClosedIssue",
	} {
		if got != want {
			t.Fatalf("Step type = %q, want %q", got, want)
		}
	}
}

func TestFindIssueWithTitleIgnoresCaseAndSurroundingWhitespace(t *testing.T) {
	issues := []github.Issue{{Number: 3, Title: "Document the export format"}, {Number: 7, Title: "  Crash When Saving An Empty Draft "}}
	issue, isTracked := findIssueWithTitle(issues, "crash when saving an empty draft")
	if !isTracked || issue.Number != 7 {
		t.Fatalf("found issue %d, tracked %v; want issue 7", issue.Number, isTracked)
	}
	if _, isTracked := findIssueWithTitle(issues, "Crash when saving"); isTracked {
		t.Fatal("a title prefix is a different issue")
	}
}

func TestOpenIssuePageRequestCarriesTheRepositoryAndLabels(t *testing.T) {
	request := openIssuePageRequest(Input{Owner: "octocat", Repository: "hello-world", Title: "Crash", Labels: []string{"bug"}}, 2)
	if request.Owner != "octocat" || request.Repository != "hello-world" || len(request.Labels) != 1 || request.Labels[0] != "bug" || request.Page != 2 {
		t.Fatalf("request = %+v", request)
	}
}

func TestIsClosingStateReasonAcceptsOnlyCloseReasons(t *testing.T) {
	for stateReason, want := range map[string]bool{"": true, "completed": true, "not_planned": true, "reopened": false, "Completed": false} {
		if got := isClosingStateReason(stateReason); got != want {
			t.Fatalf("isClosingStateReason(%q) = %v, want %v", stateReason, got, want)
		}
	}
}
