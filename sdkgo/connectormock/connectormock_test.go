// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package connectormock_test

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/connectormock"
	"github.com/superdurable/dex-connectors-library/sdkgo/internal/testsupport"
)

const (
	widgetFound     sdkgo.BranchID = "found"
	widgetAbsent    sdkgo.BranchID = "absent"
	widgetCreated   sdkgo.BranchID = "created"
	widgetRejected  sdkgo.BranchID = "rejected"
	widgetsListed   sdkgo.BranchID = "listed"
	undeclaredRoute sdkgo.BranchID = "vanished"
)

var (
	widgetConnection = sdkgo.ConnectionRef{Provider: "widgets", Name: "primary"}
	lookupDefinition = sdkgo.QueryDefinition{
		Operation: sdkgo.OperationRef{ConnectorID: "widgets", OperationID: "lookupWidget"},
		Branches: []sdkgo.BranchDefinition{
			{ID: widgetFound, Description: "The widget exists."},
			{ID: widgetAbsent, Description: "The widget does not exist.", Optional: true},
			{ID: sdkgo.DefectBranchID, Description: "The input is invalid.", Optional: true},
		},
	}
	listDefinition = sdkgo.QueryDefinition{
		Operation: sdkgo.OperationRef{ConnectorID: "widgets", OperationID: "listWidgets"},
		Branches: []sdkgo.BranchDefinition{
			{ID: widgetsListed, Description: "One page of widgets was listed."},
			{ID: sdkgo.DefectBranchID, Description: "The input is invalid.", Optional: true},
		},
	}
	createDefinition = sdkgo.MutationDefinition{
		Operation: sdkgo.OperationRef{ConnectorID: "widgets", OperationID: "createWidget"},
		Branches: []sdkgo.BranchDefinition{
			{ID: widgetCreated, Description: "The widget was created."},
			{ID: widgetRejected, Description: "The provider rejected the widget.", Optional: true},
			{ID: sdkgo.UncertainBranchID, Description: "The outcome is unknown.", Optional: true},
			{ID: sdkgo.DefectBranchID, Description: "The input is invalid.", Optional: true},
		},
	}
)

type widgetInput struct {
	Name string `json:"name"`
}

type widget struct {
	ID string `json:"id"`
}

type widgetPageInput struct {
	PageToken string `json:"pageToken,omitempty"`
}

type widgetPage struct {
	IDs           []string `json:"ids"`
	NextPageToken string   `json:"nextPageToken,omitempty"`
}

func TestQueryAnswersRespondCasesInOrderThenRepeatsTheLastDefault(t *testing.T) {
	lookup := connectormock.NewQuery[widgetInput, widget](t, lookupDefinition)
	lookup.Default(connectormock.Branch(widgetFound, widget{ID: "default-1"}, nil), connectormock.Branch(widgetFound, widget{ID: "default-2"}, nil))
	lookup.Respond(connectormock.Branch(widgetAbsent, widget{}, nil), connectormock.Branch(widgetFound, widget{ID: "scripted"}, nil))
	flowID := uniqueFlowID(t)

	values := make([]string, 0, 5)
	branches := make([]sdkgo.BranchID, 0, 5)
	for step := range 5 {
		result := runQuery(t, lookup, flowID, step, widgetInput{Name: "gear"})
		values, branches = append(values, result.Value.ID), append(branches, result.Branch)
	}

	require.Equal(t, []sdkgo.BranchID{widgetAbsent, widgetFound, widgetFound, widgetFound, widgetFound}, branches)
	require.Equal(t, []string{"", "scripted", "default-1", "default-2", "default-2"}, values)
	calls := lookup.Calls()
	require.Len(t, calls, 5)
	require.Equal(t, flowID, calls[0].FlowID)
	require.Equal(t, widgetConnection, calls[0].Connection)
	require.Equal(t, widgetInput{Name: "gear"}, calls[0].Input)
	require.Equal(t, widgetAbsent, calls[0].Branch)
	require.NotEmpty(t, calls[0].CallID)
	require.NotEqual(t, calls[0].CallID, calls[1].CallID)
	require.Empty(t, calls[0].IdempotencyKey)
}

func TestQueryUnscriptedCallFailsTheTestAndSelectsDefect(t *testing.T) {
	recorder := newRecordingT(t)
	lookup := connectormock.NewQuery[widgetInput, widget](recorder, lookupDefinition)
	flowID := uniqueFlowID(t)

	result := runQuery(t, lookup, flowID, 0, widgetInput{Name: "gear"})

	require.Equal(t, sdkgo.DefectBranchID, result.Branch)
	require.Equal(t, sdkgo.FailureLocalDefect, result.Failure.Kind)
	require.Equal(t, "widgets", result.Failure.Provider)
	require.Equal(t, "lookupWidget", result.Failure.Operation)
	recorder.requireFailure(t, `unscripted call for Flow "`+flowID+`"`)
	calls := lookup.Calls()
	require.Len(t, calls, 1)
	require.True(t, calls[0].IsUnscripted)
	require.Equal(t, sdkgo.DefectBranchID, calls[0].Branch)
	recorded := recordedCallsForFlow(flowID)
	require.Equal(t, []connectormock.RecordedCall{{
		ConnectorID: "widgets", OperationID: "lookupWidget", ConnectionName: "primary", FlowID: flowID,
		CallID: calls[0].CallID, Branch: sdkgo.DefectBranchID, IsUnscripted: true,
	}}, recorded)
}

func TestStrictQueryIgnoresDefaults(t *testing.T) {
	recorder := newRecordingT(t)
	lookup := connectormock.NewQuery[widgetInput, widget](recorder, lookupDefinition)
	lookup.Default(connectormock.Branch(widgetFound, widget{ID: "default"}, nil)).Strict()
	lookup.ForFlow("strict-").Default(connectormock.Branch(widgetFound, widget{ID: "flow-default"}, nil))
	lookup.Respond(connectormock.Branch(widgetAbsent, widget{}, nil))
	flowID := "strict-" + uniqueFlowID(t)

	scripted := runQuery(t, lookup, flowID, 0, widgetInput{})
	unscripted := runQuery(t, lookup, flowID, 1, widgetInput{})

	require.Equal(t, widgetAbsent, scripted.Branch)
	require.Equal(t, sdkgo.DefectBranchID, unscripted.Branch)
	recorder.requireFailure(t, "the mock is strict")
}

func TestWhenRulesPrecedeTheRespondQueueAndAreNeverConsumed(t *testing.T) {
	lookup := connectormock.NewQuery[widgetInput, widget](t, lookupDefinition)
	lookup.Respond(connectormock.Branch(widgetFound, widget{ID: "queued"}, nil))
	lookup.When(func(input widgetInput) bool { return input.Name == "missing" }, connectormock.Branch(widgetAbsent, widget{}, nil))
	flowID := uniqueFlowID(t)

	first := runQuery(t, lookup, flowID, 0, widgetInput{Name: "missing"})
	second := runQuery(t, lookup, flowID, 1, widgetInput{Name: "missing"})
	third := runQuery(t, lookup, flowID, 2, widgetInput{Name: "gear"})

	require.Equal(t, widgetAbsent, first.Branch)
	require.Equal(t, widgetAbsent, second.Branch)
	require.Equal(t, widgetFound, third.Branch)
	require.Equal(t, "queued", third.Value.ID)
}

func TestForFlowPrefersTheLongestMatchAndFallsThroughToTheMockScript(t *testing.T) {
	lookup := connectormock.NewQuery[widgetInput, widget](t, lookupDefinition)
	base := uniqueFlowID(t)
	lookup.Default(connectormock.Branch(widgetFound, widget{ID: "mock-default"}, nil))
	lookup.ForFlow(base).Respond(connectormock.Branch(widgetFound, widget{ID: "prefix"}, nil))
	lookup.ForFlow(base + "-order-1").Respond(connectormock.Branch(widgetFound, widget{ID: "exact"}, nil))
	lookup.ForFlow(base + "-order-2").Default(connectormock.Branch(widgetAbsent, widget{}, nil))

	exact := runQuery(t, lookup, base+"-order-1", 0, widgetInput{})
	prefix := runQuery(t, lookup, base+"-order-1", 1, widgetInput{})
	fallThrough := runQuery(t, lookup, base+"-order-1", 2, widgetInput{})
	scopedDefault := runQuery(t, lookup, base+"-order-2", 0, widgetInput{})
	otherFlow := runQuery(t, lookup, "other-"+base, 0, widgetInput{})

	require.Equal(t, "exact", exact.Value.ID)
	require.Equal(t, "prefix", prefix.Value.ID)
	require.Equal(t, "mock-default", fallThrough.Value.ID)
	require.Equal(t, widgetAbsent, scopedDefault.Branch)
	require.Equal(t, "mock-default", otherFlow.Value.ID)
	require.Same(t, lookup.ForFlow(base), lookup.ForFlow(base))
}

func TestInvalidCasesFailScriptingAndStillSelectDefectThroughTheSDK(t *testing.T) {
	recorder := newRecordingT(t)
	lookup := connectormock.NewQuery[widgetInput, widget](recorder, lookupDefinition)
	lookup.Respond(
		connectormock.Branch(undeclaredRoute, widget{ID: "lost"}, nil),
		connectormock.Uncertain(widget{}, sdkgo.Failure{Kind: sdkgo.FailureTransport, Message: "lost"}),
		connectormock.Branch(widgetAbsent, widget{}, &sdkgo.Failure{Kind: "UNKNOWN", Message: "bad kind"}),
		connectormock.Case[widget]{},
	)
	flowID := uniqueFlowID(t)

	branches := make([]sdkgo.BranchID, 0, 4)
	for step := range 4 {
		branches = append(branches, runQuery(t, lookup, flowID, step, widgetInput{}).Branch)
	}

	require.Equal(t, []sdkgo.BranchID{sdkgo.DefectBranchID, sdkgo.DefectBranchID, sdkgo.DefectBranchID, sdkgo.DefectBranchID}, branches)
	recorder.requireFailure(t, `branch "vanished" is not declared by the operation`)
	recorder.requireFailure(t, "an uncertain case is valid only for a mutation")
	recorder.requireFailure(t, "the case failure is invalid")
	recorder.requireFailure(t, "the case was not built with Branch, Retry, Uncertain, or Paged")
	for _, call := range lookup.Calls() {
		require.Equal(t, sdkgo.DefectBranchID, call.Branch)
		require.False(t, call.IsUnscripted)
	}
}

func TestRetryAsksDexToRetryAndCompletesItsFailure(t *testing.T) {
	lookup := connectormock.NewQuery[widgetInput, widget](t, lookupDefinition)
	lookup.Respond(connectormock.Retry[widget](sdkgo.Failure{Kind: sdkgo.FailureRateLimit, Message: "slow down"}, 2*time.Second))
	flowID := uniqueFlowID(t)

	_, err := sdkgo.RunQuery(testsupport.NewDexContext(flowID, "step-0"), lookup, widgetConnection, widgetInput{})

	var retryError *sdkgo.RetryError
	require.ErrorAs(t, err, &retryError)
	require.Equal(t, sdkgo.Failure{
		Kind: sdkgo.FailureRateLimit, Provider: "widgets", Operation: "lookupWidget", Message: "slow down",
	}, retryError.Failure)
	calls := lookup.Calls()
	require.Len(t, calls, 1)
	require.True(t, calls[0].IsRetry)
	require.Empty(t, calls[0].Branch)
}

func TestMutationReplaysTheFirstTerminalOutcomeForARepeatedIdempotencyKey(t *testing.T) {
	create := connectormock.NewMutation[widgetInput, widget](t, createDefinition)
	create.Respond(
		connectormock.Retry[widget](sdkgo.Failure{Kind: sdkgo.FailureAvailability, Message: "try again"}, 0),
		connectormock.Branch(widgetCreated, widget{ID: "widget-1"}, nil),
		connectormock.Uncertain(widget{}, sdkgo.Failure{Kind: sdkgo.FailureTransport, Message: "connection lost"}),
	)
	flowID := uniqueFlowID(t)

	_, retryErr := runMutation(t, create, flowID, "send-1", widgetInput{Name: "gear"})
	created, createdErr := runMutation(t, create, flowID, "send-1", widgetInput{Name: "gear"})
	replayed, replayedErr := runMutation(t, create, flowID, "send-1", widgetInput{Name: "gear"})
	uncertain, uncertainErr := runMutation(t, create, flowID, "send-2", widgetInput{Name: "bolt"})
	uncertainReplay, uncertainReplayErr := runMutation(t, create, flowID, "send-2", widgetInput{Name: "bolt"})

	var retryError *sdkgo.RetryError
	require.ErrorAs(t, retryErr, &retryError)
	require.NoError(t, errors.Join(createdErr, replayedErr, uncertainErr, uncertainReplayErr))
	require.Equal(t, widgetCreated, created.Branch)
	require.Equal(t, created.Value, replayed.Value)
	require.Equal(t, widgetCreated, replayed.Branch)
	require.Equal(t, sdkgo.UncertainBranchID, uncertain.Branch)
	require.Equal(t, sdkgo.FailureTransport, uncertain.Failure.Kind)
	require.Equal(t, sdkgo.UncertainBranchID, uncertainReplay.Branch)
	calls := create.Calls()
	require.Len(t, calls, 5)
	require.Equal(t, []bool{false, false, true, false, true}, []bool{
		calls[0].IsReplay, calls[1].IsReplay, calls[2].IsReplay, calls[3].IsReplay, calls[4].IsReplay,
	})
	require.Equal(t, sdkgo.IdempotencyKey(calls[0].CallID), calls[0].IdempotencyKey)
	require.Equal(t, calls[0].IdempotencyKey, calls[2].IdempotencyKey)
	require.NotEqual(t, calls[0].IdempotencyKey, calls[3].IdempotencyKey)
}

func TestUncertainRequiresTheMutationToDeclareTheBranch(t *testing.T) {
	recorder := newRecordingT(t)
	definition := createDefinition
	definition.Branches = []sdkgo.BranchDefinition{
		{ID: widgetCreated, Description: "The widget was created."},
		{ID: sdkgo.DefectBranchID, Description: "The input is invalid.", Optional: true},
	}
	create := connectormock.NewMutation[widgetInput, widget](recorder, definition)
	create.Respond(connectormock.Uncertain(widget{}, sdkgo.Failure{Kind: sdkgo.FailureTransport, Message: "lost"}))

	result, err := runMutation(t, create, uniqueFlowID(t), "send-1", widgetInput{})

	require.NoError(t, err)
	require.Equal(t, sdkgo.DefectBranchID, result.Branch)
	recorder.requireFailure(t, "the mutation does not declare the uncertain branch")
}

func TestPagedCaseFollowsTheListingCursor(t *testing.T) {
	recorder := newRecordingT(t)
	pages := []widgetPage{
		{IDs: []string{"w-3", "w-1"}, NextPageToken: "token-b"},
		{IDs: []string{"w-2", "w-2"}, NextPageToken: "token-c"},
		{IDs: []string{}},
	}
	list := connectormock.NewQuery[widgetPageInput, widgetPage](recorder, listDefinition)
	list.Default(connectormock.Paged(widgetsListed, connectormock.CursorPageNumber[widgetPageInput](
		"pageToken", "nextPageToken", pages), pages...))
	flowID := uniqueFlowID(t)

	first := runQuery(t, list, flowID, 0, widgetPageInput{})
	second := runQuery(t, list, flowID, 1, widgetPageInput{PageToken: first.Value.NextPageToken})
	third := runQuery(t, list, flowID, 2, widgetPageInput{PageToken: second.Value.NextPageToken})
	outside := runQuery(t, list, flowID, 3, widgetPageInput{PageToken: "token-z"})

	require.Equal(t, pages[0], first.Value)
	require.Equal(t, pages[1], second.Value)
	require.Equal(t, pages[2], third.Value)
	require.Equal(t, widgetsListed, third.Branch)
	require.Equal(t, sdkgo.DefectBranchID, outside.Branch)
	recorder.requireFailure(t, "the input requests page 0, but the paged case has 3 pages")
}

func TestCursorPageNumberAcceptsNumberedPages(t *testing.T) {
	type numberedInput struct {
		Page int `json:"page,omitempty"`
	}
	type numberedPage struct {
		NextPage int `json:"nextPage"`
	}
	pageNumber := connectormock.CursorPageNumber[numberedInput]("page", "nextPage", []numberedPage{{NextPage: 2}, {NextPage: 3}, {}})

	require.Equal(t, []int{1, 2, 3, 0}, []int{
		pageNumber(numberedInput{}), pageNumber(numberedInput{Page: 2}), pageNumber(numberedInput{Page: 3}), pageNumber(numberedInput{Page: 4}),
	})
}

func TestPagedCaseRejectsAnotherInputType(t *testing.T) {
	recorder := newRecordingT(t)
	list := connectormock.NewQuery[widgetPageInput, widgetPage](recorder, listDefinition)

	list.Respond(connectormock.Paged(widgetsListed, func(widgetInput) int { return 1 }, widgetPage{}))

	recorder.requireFailure(t, "the paged case reads connectormock_test.widgetInput input, but the operation takes connectormock_test.widgetPageInput")
}

func TestDecodeOutputIsStrictAndRunsTheOutputValidation(t *testing.T) {
	decoded, err := connectormock.DecodeOutput[widget](`{"id": "w-1"}`)
	require.NoError(t, err)
	require.Equal(t, widget{ID: "w-1"}, decoded)

	_, err = connectormock.DecodeOutput[widget](`{"id": "w-1", "color": "red"}`)
	require.ErrorContains(t, err, `unknown field "color"`)
	_, err = connectormock.DecodeOutput[widget](`{"id": "w-1"} {}`)
	require.ErrorContains(t, err, "trailing data")
	_, err = connectormock.DecodeOutput[validatedWidget](`{"id": ""}`)
	require.ErrorContains(t, err, "widget ID is required")
	require.Panics(t, func() { connectormock.MustDecodeOutput[widget](`[]`) })
}

func TestMockStopsReportingAfterItsTestFinished(t *testing.T) {
	var lookup *connectormock.Query[widgetInput, widget]
	t.Run("finished", func(subtest *testing.T) {
		lookup = connectormock.NewQuery[widgetInput, widget](subtest, lookupDefinition)
	})

	result := runQuery(t, lookup, uniqueFlowID(t), 0, widgetInput{})

	require.Equal(t, sdkgo.DefectBranchID, result.Branch)
	require.True(t, lookup.Calls()[0].IsUnscripted)
}

func TestQueryIsSafeForConcurrentCalls(t *testing.T) {
	lookup := connectormock.NewQuery[widgetInput, widget](t, lookupDefinition)
	lookup.Default(connectormock.Branch(widgetFound, widget{ID: "shared"}, nil))
	flowID := uniqueFlowID(t)
	errs := make([]error, 16)
	var group sync.WaitGroup
	for step := range errs {
		group.Add(1)
		go func() {
			defer group.Done()
			_, errs[step] = sdkgo.RunQuery(testsupport.NewDexContext(flowID, fmt.Sprintf("step-%d", step)), lookup, widgetConnection, widgetInput{})
		}()
	}
	group.Wait()

	require.NoError(t, errors.Join(errs...))
	require.Len(t, lookup.Calls(), 16)
}

type validatedWidget struct {
	ID string `json:"id"`
}

func (value validatedWidget) Validate() error {
	if value.ID == "" {
		return errors.New("widget ID is required")
	}
	return nil
}

func runQuery[IN, OUT any](t *testing.T, query sdkgo.Query[IN, OUT], flowID string, step int, input IN) sdkgo.QueryResult[OUT] {
	t.Helper()
	result, err := sdkgo.RunQuery(testsupport.NewDexContext(flowID, fmt.Sprintf("step-%d", step)), query, widgetConnection, input)
	require.NoError(t, err)
	return result
}

func runMutation[IN, OUT any](t *testing.T, mutation sdkgo.Mutation[IN, OUT], flowID, stepExecutionID string, input IN) (sdkgo.MutationResult[OUT], error) {
	t.Helper()
	return sdkgo.RunMutation(testsupport.NewDexContext(flowID, stepExecutionID), mutation, widgetConnection, input)
}

func uniqueFlowID(t *testing.T) string {
	return strings.ReplaceAll(t.Name(), "/", "-") + "-" + fmt.Sprint(time.Now().UnixNano())
}

func recordedCallsForFlow(flowID string) []connectormock.RecordedCall {
	matching := make([]connectormock.RecordedCall, 0)
	for _, call := range connectormock.RecordedCalls() {
		if call.FlowID == flowID {
			matching = append(matching, call)
		}
	}
	return matching
}

// recordingT records failures that a test expects instead of failing it.
type recordingT struct {
	testing.TB
	mu       sync.Mutex
	failures []string
}

func newRecordingT(t *testing.T) *recordingT {
	return &recordingT{TB: t}
}

func (recorder *recordingT) Helper() {}

func (recorder *recordingT) Errorf(format string, values ...any) {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	recorder.failures = append(recorder.failures, fmt.Sprintf(format, values...))
}

func (recorder *recordingT) requireFailure(t *testing.T, expected string) {
	t.Helper()
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	for _, failure := range recorder.failures {
		if strings.Contains(failure, expected) {
			return
		}
	}
	t.Fatalf("no recorded failure contains %q; failures: %q", expected, recorder.failures)
}
