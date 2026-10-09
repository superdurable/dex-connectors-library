// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package connectormock

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// scriptedOperation owns the script, call log, and idempotency memory shared
// by Query and Mutation mocks.
type scriptedOperation[IN, OUT any] struct {
	t                testing.TB
	operation        sdkgo.OperationRef
	declaredBranches map[sdkgo.BranchID]bool
	isMutation       bool
	mu               sync.Mutex
	unscopedScript   script[IN, OUT]
	flowScripts      map[string]*FlowScript[IN, OUT]
	isStrict         bool
	isTestFinished   bool
	calls            []Call[IN]
	terminalByKey    map[sdkgo.IdempotencyKey]resolvedCase[OUT]
}

type script[IN, OUT any] struct {
	rules        []rule[IN, OUT]
	queue        []Case[OUT]
	defaults     []Case[OUT]
	defaultCount int
}

type rule[IN, OUT any] struct {
	matches  func(IN) bool
	response Case[OUT]
}

// resolvedCase is the outcome a mock returns for one call.
type resolvedCase[OUT any] struct {
	kind         caseKind
	branch       sdkgo.BranchID
	value        OUT
	failure      *sdkgo.Failure
	retryAfter   time.Duration
	isUnscripted bool
	isReplay     bool
	// effectiveBranch is the branch the SDK selects after validating the outcome, or empty for a Retry.
	effectiveBranch sdkgo.BranchID
}

func newScriptedOperation[IN, OUT any](t testing.TB, operation sdkgo.OperationRef, branches []sdkgo.BranchDefinition, isMutation bool) *scriptedOperation[IN, OUT] {
	declared := make(map[sdkgo.BranchID]bool, len(branches))
	for _, branch := range branches {
		declared[branch.ID] = true
	}
	scripted := &scriptedOperation[IN, OUT]{
		t: t, operation: operation, declaredBranches: declared, isMutation: isMutation,
		flowScripts: make(map[string]*FlowScript[IN, OUT]), terminalByKey: make(map[sdkgo.IdempotencyKey]resolvedCase[OUT]),
	}
	t.Cleanup(scripted.finishTest)
	return scripted
}

func (scripted *scriptedOperation[IN, OUT]) respond(target *script[IN, OUT], cases []Case[OUT]) {
	scripted.t.Helper()
	scripted.reportInvalidCases(cases...)
	scripted.mu.Lock()
	defer scripted.mu.Unlock()
	target.queue = append(target.queue, cases...)
}

func (scripted *scriptedOperation[IN, OUT]) when(target *script[IN, OUT], matches func(IN) bool, response Case[OUT]) {
	scripted.t.Helper()
	if matches == nil {
		scripted.t.Errorf("connector mock %s: When requires an input predicate", scripted.describe())
		return
	}
	scripted.reportInvalidCases(response)
	scripted.mu.Lock()
	defer scripted.mu.Unlock()
	target.rules = append(target.rules, rule[IN, OUT]{matches: matches, response: response})
}

func (scripted *scriptedOperation[IN, OUT]) setDefault(target *script[IN, OUT], cases []Case[OUT]) {
	scripted.t.Helper()
	scripted.reportInvalidCases(cases...)
	scripted.mu.Lock()
	defer scripted.mu.Unlock()
	target.defaults = cases
	target.defaultCount = 0
}

func (scripted *scriptedOperation[IN, OUT]) makeStrict() {
	scripted.mu.Lock()
	defer scripted.mu.Unlock()
	scripted.isStrict = true
}

func (scripted *scriptedOperation[IN, OUT]) flowScript(flowIDOrPrefix string) *FlowScript[IN, OUT] {
	scripted.t.Helper()
	if strings.TrimSpace(flowIDOrPrefix) == "" {
		scripted.t.Errorf("connector mock %s: ForFlow requires a Flow ID or prefix", scripted.describe())
	}
	scripted.mu.Lock()
	defer scripted.mu.Unlock()
	if existing := scripted.flowScripts[flowIDOrPrefix]; existing != nil {
		return existing
	}
	created := &FlowScript[IN, OUT]{operation: scripted, flowIDOrPrefix: flowIDOrPrefix}
	scripted.flowScripts[flowIDOrPrefix] = created
	return created
}

func (scripted *scriptedOperation[IN, OUT]) recordedCalls() []Call[IN] {
	scripted.mu.Lock()
	defer scripted.mu.Unlock()
	return append([]Call[IN](nil), scripted.calls...)
}

// answer resolves one call. A Mutation passes its idempotency key; a Query
// passes an empty key and never replays.
func (scripted *scriptedOperation[IN, OUT]) answer(call sdkgo.Call, input IN, idempotencyKey sdkgo.IdempotencyKey) resolvedCase[OUT] {
	flowID := ""
	if call.Context != nil {
		flowID = call.Context.FlowID()
	}
	scripted.mu.Lock()
	defer scripted.mu.Unlock()
	resolved, isRemembered := scripted.terminalByKey[idempotencyKey]
	if idempotencyKey != "" && isRemembered {
		resolved.isReplay = true
	} else {
		resolved = scripted.resolveScriptedCase(flowID, input)
		if idempotencyKey != "" && resolved.kind != caseKindRetry {
			scripted.terminalByKey[idempotencyKey] = resolved
		}
	}
	recorded := Call[IN]{
		FlowID: flowID, CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Connection: call.Connection,
		Input: input, Branch: resolved.effectiveBranch, IsRetry: resolved.kind == caseKindRetry,
		IsReplay: resolved.isReplay, IsUnscripted: resolved.isUnscripted,
	}
	scripted.calls = append(scripted.calls, recorded)
	recordProcessCall(RecordedCall{
		ConnectorID: scripted.operation.ConnectorID, OperationID: scripted.operation.OperationID,
		ConnectionName: call.Connection.Name, FlowID: flowID, CallID: call.ID,
		Branch: recorded.Branch, IsRetry: recorded.IsRetry, IsReplay: recorded.IsReplay, IsUnscripted: recorded.IsUnscripted,
	})
	return resolved
}

// resolveScriptedCase applies the script precedence: the most specific
// ForFlow script, then the unscoped script; within each, When rules before
// the Respond queue; then defaults in the same order unless the mock is
// strict.
func (scripted *scriptedOperation[IN, OUT]) resolveScriptedCase(flowID string, input IN) resolvedCase[OUT] {
	scripts := scripted.scriptsFor(flowID)
	for _, candidate := range scripts {
		if response, found := candidate.scriptedCase(input); found {
			return scripted.resolveCase(response, input)
		}
	}
	if !scripted.isStrict {
		for _, candidate := range scripts {
			if response, found := candidate.defaultCase(); found {
				return scripted.resolveCase(response, input)
			}
		}
	}
	message := fmt.Sprintf("unscripted call for Flow %q; script it with Respond, When, or Default", flowID)
	if scripted.isStrict {
		message = fmt.Sprintf("unscripted call for Flow %q; the mock is strict, so script it with Respond or When", flowID)
	}
	scripted.reportFailure(message)
	resolved := scripted.defectCase("unscripted connector mock call")
	resolved.isUnscripted = true
	return resolved
}

func (scripted *scriptedOperation[IN, OUT]) scriptsFor(flowID string) []*script[IN, OUT] {
	matching := make([]*FlowScript[IN, OUT], 0, len(scripted.flowScripts))
	for flowIDOrPrefix, flowScript := range scripted.flowScripts {
		if flowIDOrPrefix != "" && strings.HasPrefix(flowID, flowIDOrPrefix) {
			matching = append(matching, flowScript)
		}
	}
	sort.Slice(matching, func(left, right int) bool {
		return len(matching[left].flowIDOrPrefix) > len(matching[right].flowIDOrPrefix)
	})
	scripts := make([]*script[IN, OUT], 0, len(matching)+1)
	for _, flowScript := range matching {
		scripts = append(scripts, &flowScript.script)
	}
	return append(scripts, &scripted.unscopedScript)
}

func (scripted *scriptedOperation[IN, OUT]) resolveCase(response Case[OUT], input IN) resolvedCase[OUT] {
	if response.kind == caseKindPaged {
		return scripted.resolvePage(response, input)
	}
	resolved := resolvedCase[OUT]{
		kind: response.kind, branch: response.branch, value: response.value,
		failure: scripted.completeFailure(response.failure), retryAfter: response.retryAfter,
	}
	if problem := scripted.caseProblem(response); problem != "" {
		resolved.effectiveBranch = sdkgo.DefectBranchID
		if response.kind == caseKindInvalid {
			return scripted.defectCase(problem)
		}
		return resolved
	}
	if response.kind != caseKindRetry {
		resolved.effectiveBranch = response.branch
	}
	return resolved
}

func (scripted *scriptedOperation[IN, OUT]) resolvePage(response Case[OUT], input IN) resolvedCase[OUT] {
	if problem := scripted.caseProblem(response); problem != "" {
		return scripted.defectCase(problem)
	}
	pageNumber := response.pageNumber(input)
	if pageNumber < 1 || pageNumber > len(response.pages) {
		scripted.reportFailure(fmt.Sprintf("the input requests page %d, but the paged case has %d pages", pageNumber, len(response.pages)))
		return scripted.defectCase("requested page is outside the scripted listing")
	}
	return resolvedCase[OUT]{
		kind: caseKindBranch, branch: response.branch, value: response.pages[pageNumber-1],
		effectiveBranch: response.branch,
	}
}

func (scripted *scriptedOperation[IN, OUT]) defectCase(message string) resolvedCase[OUT] {
	failure := sdkgo.Failure{
		Kind: sdkgo.FailureLocalDefect, Provider: scripted.operation.ConnectorID,
		Operation: scripted.operation.OperationID, Message: message,
	}
	return resolvedCase[OUT]{
		kind: caseKindBranch, branch: sdkgo.DefectBranchID, failure: &failure, effectiveBranch: sdkgo.DefectBranchID,
	}
}

// caseProblem returns why the SDK would answer this case with the defect
// branch, or "" when the case is valid for the operation.
func (scripted *scriptedOperation[IN, OUT]) caseProblem(response Case[OUT]) string {
	switch response.kind {
	case caseKindBranch, caseKindPaged:
		if !scripted.declaredBranches[response.branch] {
			return fmt.Sprintf("branch %q is not declared by the operation", response.branch)
		}
		if response.kind == caseKindPaged {
			if inputType := reflect.TypeFor[IN](); response.inputType != inputType {
				return fmt.Sprintf("the paged case reads %v input, but the operation takes %v", response.inputType, inputType)
			}
			if len(response.pages) == 0 {
				return "the paged case has no pages"
			}
		}
	case caseKindRetry:
		if response.retryAfter < 0 {
			return "the retry delay is negative"
		}
	case caseKindUncertain:
		if !scripted.isMutation {
			return "an uncertain case is valid only for a mutation"
		}
		if !scripted.declaredBranches[sdkgo.UncertainBranchID] {
			return "the mutation does not declare the uncertain branch"
		}
	default:
		return "the case was not built with Branch, Retry, Uncertain, or Paged"
	}
	if response.failure != nil {
		if err := scripted.completeFailure(response.failure).Validate(); err != nil {
			return fmt.Sprintf("the case failure is invalid: %v", err)
		}
	}
	return ""
}

// completeFailure returns a copy whose empty Provider and Operation name this operation.
func (scripted *scriptedOperation[IN, OUT]) completeFailure(failure *sdkgo.Failure) *sdkgo.Failure {
	if failure == nil {
		return nil
	}
	completed := *failure
	if completed.Provider == "" {
		completed.Provider = scripted.operation.ConnectorID
	}
	if completed.Operation == "" {
		completed.Operation = scripted.operation.OperationID
	}
	return &completed
}

func (scripted *scriptedOperation[IN, OUT]) reportInvalidCases(cases ...Case[OUT]) {
	scripted.t.Helper()
	for _, response := range cases {
		if problem := scripted.caseProblem(response); problem != "" {
			scripted.t.Errorf("connector mock %s: %s", scripted.describe(), problem)
		}
	}
}

// reportFailure fails the test unless it already finished, because a Worker
// may still answer calls while the test's cleanup stops it.
func (scripted *scriptedOperation[IN, OUT]) reportFailure(message string) {
	if scripted.isTestFinished {
		return
	}
	scripted.t.Errorf("connector mock %s: %s", scripted.describe(), message)
}

func (scripted *scriptedOperation[IN, OUT]) finishTest() {
	scripted.mu.Lock()
	defer scripted.mu.Unlock()
	scripted.isTestFinished = true
}

func (scripted *scriptedOperation[IN, OUT]) describe() string {
	return scripted.operation.ConnectorID + "." + scripted.operation.OperationID
}

func (current *script[IN, OUT]) scriptedCase(input IN) (Case[OUT], bool) {
	for _, candidate := range current.rules {
		if candidate.matches(input) {
			return candidate.response, true
		}
	}
	if len(current.queue) == 0 {
		return Case[OUT]{}, false
	}
	next := current.queue[0]
	current.queue = current.queue[1:]
	return next, true
}

// defaultCase returns the defaults in order and then repeats the last one.
func (current *script[IN, OUT]) defaultCase() (Case[OUT], bool) {
	if len(current.defaults) == 0 {
		return Case[OUT]{}, false
	}
	index := min(current.defaultCount, len(current.defaults)-1)
	current.defaultCount++
	return current.defaults[index], true
}
