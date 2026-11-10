// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/superdurable/dex-connectors-library/connectorsmcp/internal/attemptcontext"
	"github.com/superdurable/dex-connectors-library/connectorsmcp/internal/callstate"
	"github.com/superdurable/dex-connectors-library/connectorsmcp/internal/canonicaljson"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// ProtocolPoint names a step of the keyed write protocol. Tests kill the
// process at these points; production observes nothing.
type ProtocolPoint string

const (
	// ProtocolPointBeforeRecordCreate is just before a new key's first record write.
	ProtocolPointBeforeRecordCreate ProtocolPoint = "before-record-create"
	// ProtocolPointAfterRecordCreate is after that write, before the attempt runs.
	ProtocolPointAfterRecordCreate ProtocolPoint = "after-record-create"
	// ProtocolPointAfterAttempt is after an attempt returned, before its result is stored.
	ProtocolPointAfterAttempt ProtocolPoint = "after-attempt"
	// ProtocolPointAfterResultStored is after the final result is stored, before the response.
	ProtocolPointAfterResultStored ProtocolPoint = "after-result-stored"
)

// executorConfig holds the call limits of a toolCallExecutor.
type executorConfig struct {
	serverVersion         string
	callTimeBudget        time.Duration
	callStateRetention    time.Duration
	maxResultBytes        int
	maximumInlineAttempts int32
}

// toolCallExecutor runs tools/call requests: reads directly, declared writes
// under their idempotency key's call state.
type toolCallExecutor struct {
	store                *callstate.Store
	clock                Clock
	config               executorConfig
	logger               *slog.Logger
	observeProtocolPoint func(point ProtocolPoint, operationID string)
	isDraining           atomic.Bool
	inflightCalls        sync.WaitGroup
}

func newToolCallExecutor(store *callstate.Store, clock Clock, config executorConfig, logger *slog.Logger, observeProtocolPoint func(ProtocolPoint, string)) *toolCallExecutor {
	if store == nil || clock == nil || logger == nil {
		panic("tool call executor requires a store, a clock, and a logger")
	}
	if observeProtocolPoint == nil {
		observeProtocolPoint = func(ProtocolPoint, string) {}
	}
	return &toolCallExecutor{store: store, clock: clock, config: config, logger: logger, observeProtocolPoint: observeProtocolPoint}
}

// beginDrain makes new calls answer that the server is stopping.
func (executor *toolCallExecutor) beginDrain() { executor.isDraining.Store(true) }

// waitForInflightCalls waits for running calls until ctx ends.
func (executor *toolCallExecutor) waitForInflightCalls(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		executor.inflightCalls.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// toolCall is the state of one tools/call request.
type toolCall struct {
	executor  *toolCallExecutor
	toolSet   *ToolSet
	name      string
	arguments json.RawMessage
	binding   *toolBinding
	startedAt time.Time
	stateKey  string
	// knownEffect is the effect a failure answer must respect.
	knownEffect sdkgo.AgentToolEffect
	attempt     int32
}

// callAnswer is either a result or a JSON-RPC error.
type callAnswer struct {
	result     toolResult
	err        error
	isReplayed bool
}

// callTool answers one tools/call. It never panics: a panic answers
// unknown for a write and an interruption for a read.
func (executor *toolCallExecutor) callTool(ctx context.Context, toolSet *ToolSet, params *mcp.CallToolParamsRaw) (result *mcp.CallToolResult, err error) {
	executor.inflightCalls.Add(1)
	defer executor.inflightCalls.Done()
	call := &toolCall{
		executor: executor, toolSet: toolSet, name: params.Name, arguments: normalizeArguments(params.Arguments),
		binding: toolSet.toolByName(params.Name), startedAt: executor.clock.Now(), knownEffect: sdkgo.AgentToolEffectNonIdempotentWrite,
	}
	if call.binding != nil {
		call.knownEffect = call.binding.effect
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			executor.logger.Error("tool call failed with a panic", "tool", call.name, "panic", fmt.Sprintf("%T", recovered))
			answer := call.answerAfterPanic()
			call.logAnswer(answer)
			result, err = answer.toMCP()
		}
	}()
	answer := call.run(ctx, params.Meta)
	call.logAnswer(answer)
	return answer.toMCP()
}

func (answer callAnswer) toMCP() (*mcp.CallToolResult, error) {
	if answer.err != nil {
		return nil, answer.err
	}
	return answer.result.toMCP(answer.isReplayed), nil
}

func (call *toolCall) run(ctx context.Context, meta mcp.Meta) callAnswer {
	key, hasKey, keyProblem := idempotencyKeyOf(meta)
	switch {
	case keyProblem != "" && call.binding == nil:
		return callAnswer{err: unknownToolError(call.name)}
	case keyProblem != "":
		return callAnswer{result: rejectedResult(keyProblem + "; nothing was sent")}
	case hasKey:
		return call.runKeyed(ctx, key)
	case call.binding == nil:
		return callAnswer{err: unknownToolError(call.name)}
	case call.binding.effect != sdkgo.AgentToolEffectRead:
		return callAnswer{result: rejectedResult("this tool changes data and needs _meta " + metaKeyIdempotencyKey + "; nothing was sent")}
	default:
		return call.runRead(ctx, uuid.NewString())
	}
}

// runKeyed looks the key's state up before anything else, so a call with
// state is answered from it even after its tool was renamed or withdrawn.
func (call *toolCall) runKeyed(ctx context.Context, key string) callAnswer {
	call.stateKey = callstate.StateKey(key)
	lock, err := call.executor.store.Lock(ctx, call.stateKey)
	if err != nil {
		return call.answerLockFailure(ctx, key, err)
	}
	record, isFound, readErr := lock.Read()
	if readErr == nil && !isFound && call.binding != nil && call.binding.effect == sdkgo.AgentToolEffectRead {
		call.releaseLock(lock)
		return call.runRead(ctx, key)
	}
	defer call.releaseLock(lock)
	switch {
	case readErr != nil:
		return call.answerUnreadableState(readErr)
	case isFound:
		return call.resumeRecordedCall(ctx, lock, &record, key)
	case call.binding == nil:
		return callAnswer{err: unknownToolError(call.name)}
	default:
		return call.startWrite(ctx, lock, key)
	}
}

func (call *toolCall) releaseLock(lock *callstate.KeyLock) {
	if err := lock.Release(); err != nil {
		call.executor.logger.Warn("call state lock was not released cleanly", "key", call.keyPrefix(), "error", err)
	}
}

func (call *toolCall) answerLockFailure(ctx context.Context, key string, err error) callAnswer {
	isWaitCanceled := errors.Is(err, callstate.ErrLockWaitCanceled)
	if !isWaitCanceled {
		call.executor.logger.Error("call state lock failed", "tool", call.name, "key", call.keyPrefix(), "error", err)
	}
	switch {
	case call.binding == nil:
		return callAnswer{err: interruptedError("call state is unavailable; nothing was sent by this call")}
	case call.binding.effect == sdkgo.AgentToolEffectRead && isWaitCanceled:
		return callAnswer{err: interruptedError("the call was canceled")}
	case call.binding.effect == sdkgo.AgentToolEffectRead:
		return call.runRead(ctx, key)
	case call.binding.effect == sdkgo.AgentToolEffectIdempotentWrite:
		return callAnswer{err: interruptedError("call state is busy or unavailable; nothing was sent by this call")}
	case isWaitCanceled:
		// Contention means an attempt of this same call is running and may send.
		return callAnswer{result: unknownResult(call.binding.operationLabel() + " may have taken effect: another attempt of this call is still running; do not repeat it")}
	case !call.executor.store.HasRecordFile(call.stateKey):
		return callAnswer{result: rejectedResult("call state is unavailable; nothing was sent")}
	default:
		return callAnswer{result: unknownResult(call.binding.operationLabel() + " may have taken effect: its call state is unavailable; do not repeat it")}
	}
}

func (call *toolCall) answerUnreadableState(err error) callAnswer {
	call.executor.logger.Error("call state record cannot be read; answering unknown", "tool", call.name, "key", call.keyPrefix(), "error", err)
	if call.binding != nil && call.binding.effect != sdkgo.AgentToolEffectNonIdempotentWrite && !callstate.IsRecordUnreadable(err) {
		return callAnswer{err: interruptedError("call state cannot be read; nothing was sent by this call")}
	}
	return callAnswer{result: unknownResult("tool " + call.name + " may have taken effect: its call state cannot be read; do not repeat it")}
}

// resumeRecordedCall answers a key that already has a record, from the
// record's own identity: replay, finalize, or the next attempt.
func (call *toolCall) resumeRecordedCall(ctx context.Context, lock *callstate.KeyLock, record *callstate.Record, key string) callAnswer {
	call.knownEffect = record.Effect
	call.attempt = record.Attempt
	if record.Request.ArgumentsDigest != digestArguments(call.arguments) {
		return callAnswer{result: rejectedResult("the idempotency key was already used for a different call; nothing was sent")}
	}
	if record.Status == callstate.StatusCompleted {
		stored, err := decodeStoredResult(record.Result)
		if err != nil {
			call.executor.logger.Error("stored call result cannot be decoded; answering unknown", "key", call.keyPrefix(), "error", err)
			return callAnswer{result: unknownResult(recordLabel(record) + " may have taken effect: its stored result cannot be read; do not repeat it")}
		}
		return callAnswer{result: stored, isReplayed: true}
	}
	recordBinding := call.toolSet.toolForRequest(record.Request)
	effect := record.Effect
	if recordBinding != nil && effectRank(recordBinding.effect) > effectRank(effect) {
		effect = recordBinding.effect
	}
	if effect != sdkgo.AgentToolEffectIdempotentWrite {
		return call.finalizeFromCheckpoint(lock, record, "an earlier attempt of this call was interrupted")
	}
	if call.executor.isDraining.Load() {
		return callAnswer{err: interruptedError("the server is stopping; nothing was sent by this call")}
	}
	if !call.executor.clock.Now().Before(record.RetryWindowEndsAt) {
		return call.finalizeFromCheckpoint(lock, record, "its retry window has passed")
	}
	if recordBinding == nil {
		return call.finalizeFromCheckpoint(lock, record, "its tool is no longer offered for this connection")
	}
	if limit := attemptLimit(retryPolicyOf(recordBinding.metadata), call.executor.config.maximumInlineAttempts); record.Attempt >= limit {
		return call.finalizeFromCheckpoint(lock, record, fmt.Sprintf("it already made %d attempts", record.Attempt))
	}
	record.Attempt++
	call.attempt = record.Attempt
	if err := lock.Write(record); err != nil {
		call.executor.logger.Error("call state cannot be written", "key", call.keyPrefix(), "error", err)
		return callAnswer{err: interruptedError("call state cannot be written; nothing was sent by this call")}
	}
	return call.runWriteAttempts(ctx, lock, record, recordBinding, key)
}

// startWrite creates a key's record durably, then runs the first attempt.
func (call *toolCall) startWrite(ctx context.Context, lock *callstate.KeyLock, key string) callAnswer {
	binding := call.binding
	isIdempotent := binding.effect == sdkgo.AgentToolEffectIdempotentWrite
	if call.executor.isDraining.Load() {
		if isIdempotent {
			return callAnswer{err: interruptedError("the server is stopping; nothing was sent")}
		}
		return callAnswer{result: rejectedResult("the server is stopping; nothing was sent")}
	}
	if problem := binding.validateArguments(call.arguments); problem != "" {
		return callAnswer{result: rejectedResult("invalid arguments for " + call.name + ": " + problem)}
	}
	now := call.executor.clock.Now().UTC()
	request := callstate.Request{
		ConnectorID: binding.connector.ConnectorID, OperationID: binding.metadata.Operation.OperationID,
		ConnectionName: binding.connectionName, ProjectIdentity: call.toolSet.projectIdentity,
		ArgumentsDigest: digestArguments(call.arguments),
	}
	record := callstate.Record{
		SchemaVersion:     callstate.FormatVersion,
		StateKey:          call.stateKey,
		Request:           request,
		RequestDigest:     request.Digest(),
		Effect:            binding.effect,
		CallID:            string(deriveCallID(localFlowID, key, binding.metadata.Operation)),
		FirstAttemptAt:    now,
		RetryWindowEndsAt: now.Add(retryWindow(retryPolicyOf(binding.metadata), call.executor.config.callStateRetention)),
		Attempt:           1,
		Status:            callstate.StatusRunning,
		ServerVersion:     call.executor.config.serverVersion,
	}
	call.attempt = 1
	call.executor.observeProtocolPoint(ProtocolPointBeforeRecordCreate, request.OperationID)
	if err := lock.Write(&record); err != nil {
		call.executor.logger.Error("call state cannot be written", "key", call.keyPrefix(), "error", err)
		if isIdempotent {
			return callAnswer{err: interruptedError("call state cannot be written; nothing was sent")}
		}
		return callAnswer{result: rejectedResult("call state cannot be written; nothing was sent")}
	}
	call.executor.observeProtocolPoint(ProtocolPointAfterRecordCreate, request.OperationID)
	return call.runWriteAttempts(ctx, lock, &record, binding, key)
}

// runWriteAttempts runs the attempt the record names and, for an idempotent
// write, further attempts while the cap, the window, and the budget allow.
func (call *toolCall) runWriteAttempts(ctx context.Context, lock *callstate.KeyLock, record *callstate.Record, binding *toolBinding, key string) callAnswer {
	policy := retryPolicyOf(binding.metadata)
	limit := attemptLimit(policy, call.executor.config.maximumInlineAttempts)
	for {
		attempt, runErr := call.runWriteAttempt(ctx, lock, record, binding, key)
		call.executor.observeProtocolPoint(ProtocolPointAfterAttempt, record.Request.OperationID)
		if runErr != nil {
			return call.finishWriteAttemptWithoutBranch(lock, record, binding, attempt, runErr)
		}
		if attempt.Outcome != sdkgo.AgentToolAttemptRetry {
			return call.finalize(lock, record, branchResult(binding, attempt, call.executor.config.maxResultBytes))
		}
		detail := retryDetail(attempt)
		if record.Effect != sdkgo.AgentToolEffectIdempotentWrite {
			return call.finalizeRetryLater(lock, record, detail)
		}
		if record.Attempt >= limit {
			return call.finalizeFromCheckpoint(lock, record, fmt.Sprintf("the provider still asked to retry after %d attempts (%s)", record.Attempt, detail))
		}
		now := call.executor.clock.Now()
		delay := retryDelay(policy, record.Attempt, attempt.RetryAfter)
		if !now.Add(delay).Before(record.RetryWindowEndsAt) {
			return call.finalizeFromCheckpoint(lock, record, "its retry window ends before the next attempt ("+detail+")")
		}
		if delay+binding.attemptTimeout() > call.remainingBudget(now) {
			return callAnswer{err: interruptedError(binding.operationLabel() + " asked to retry and the call budget is spent; repeat the call with the same idempotency key")}
		}
		if !call.executor.clock.Sleep(ctx, delay) || call.executor.isDraining.Load() {
			return callAnswer{err: interruptedError("the call ended while waiting to retry; repeat it with the same idempotency key")}
		}
		record.Attempt++
		call.attempt = record.Attempt
		if err := lock.Write(record); err != nil {
			call.executor.logger.Error("call state cannot be written", "key", call.keyPrefix(), "error", err)
			return callAnswer{err: interruptedError("call state cannot be written; nothing was sent by this call")}
		}
	}
}

// runWriteAttempt runs one attempt whose number the record already holds
// durably. A client disconnect never cancels it; the attempt timeout bounds it.
func (call *toolCall) runWriteAttempt(ctx context.Context, lock *callstate.KeyLock, record *callstate.Record, binding *toolBinding, key string) (sdkgo.AgentToolAttempt, error) {
	checkpoints := &keyedCheckpointWriter{lock: lock, record: record, attempt: record.Attempt}
	attemptContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), binding.attemptTimeout())
	defer checkpoints.close()
	defer cancel()
	dexContext, err := attemptcontext.New(attemptContext, attemptcontext.Options{
		FlowID: localFlowID, StepExecutionID: key, FirstAttemptAt: record.FirstAttemptAt,
		Attempt: record.Attempt, LastHeartbeat: record.Checkpoint, OnRecordHeartbeat: checkpoints.persist,
	})
	if err != nil {
		return sdkgo.AgentToolAttempt{}, err
	}
	return binding.tool.RunAttempt(dexContext, call.arguments)
}

func (call *toolCall) finishWriteAttemptWithoutBranch(lock *callstate.KeyLock, record *callstate.Record, binding *toolBinding, attempt sdkgo.AgentToolAttempt, runErr error) callAnswer {
	if errors.Is(runErr, sdkgo.ErrAgentToolArgumentsInvalid) {
		return call.finalize(lock, record, rejectedResult("invalid arguments for "+call.name+": "+runErr.Error()))
	}
	call.executor.logger.Warn("attempt ended without a branch", "tool", call.name, "key", call.keyPrefix(), "attempt", record.Attempt, "error", runErr)
	if !attempt.HasInvokedOperation {
		if record.Effect == sdkgo.AgentToolEffectIdempotentWrite {
			return callAnswer{err: interruptedError(binding.operationLabel() + " failed before it ran; repeat the call with the same idempotency key")}
		}
		return call.finalize(lock, record, rejectedResult(binding.operationLabel()+" failed before it ran; nothing was sent"))
	}
	return call.finalize(lock, record, unknownResult(binding.operationLabel()+" may have taken effect: the attempt ended without a result; do not repeat it"))
}

// finalizeRetryLater answers a non-idempotent write whose connector asked to retry.
func (call *toolCall) finalizeRetryLater(lock *callstate.KeyLock, record *callstate.Record, detail string) callAnswer {
	if isProvenNotSent(record) {
		return call.finalize(lock, record, rejectedResult(recordLabel(record)+": not sent; the provider asked to retry later ("+detail+")"))
	}
	return call.finalize(lock, record, unknownResult(recordLabel(record)+" may have taken effect: the provider asked to retry later ("+detail+"); do not repeat it"))
}

// finalizeFromCheckpoint ends a call without another attempt: rejected only
// when the record's attempt checkpointed notSent with no provider ID.
func (call *toolCall) finalizeFromCheckpoint(lock *callstate.KeyLock, record *callstate.Record, reason string) callAnswer {
	if isProvenNotSent(record) {
		return call.finalize(lock, record, rejectedResult(recordLabel(record)+" did not take effect: "+reason+"; nothing was sent"))
	}
	return call.finalize(lock, record, unknownResult(recordLabel(record)+" may have taken effect: "+reason+"; do not repeat it"))
}

// finalize stores result as the key's final answer. A failed store still
// answers: the record stays running and a repeated call never sends blind.
func (call *toolCall) finalize(lock *callstate.KeyLock, record *callstate.Record, result toolResult) callAnswer {
	record.Status = callstate.StatusCompleted
	record.Result = result.encode()
	if err := lock.Write(record); err != nil {
		call.executor.logger.Warn("call result was not stored; a repeated call reads back or answers unknown", "key", call.keyPrefix(), "error", err)
	}
	call.executor.observeProtocolPoint(ProtocolPointAfterResultStored, record.Request.OperationID)
	return callAnswer{result: result}
}

// runRead runs a read with up to the attempt cap of inline retries. It keeps
// no state and honors client cancellation.
func (call *toolCall) runRead(ctx context.Context, stepExecutionID string) callAnswer {
	binding := call.binding
	call.knownEffect = sdkgo.AgentToolEffectRead
	if call.executor.isDraining.Load() {
		return callAnswer{err: interruptedError("the server is stopping")}
	}
	if problem := binding.validateArguments(call.arguments); problem != "" {
		return callAnswer{result: rejectedResult("invalid arguments for " + call.name + ": " + problem)}
	}
	policy := retryPolicyOf(binding.metadata)
	limit := attemptLimit(policy, call.executor.config.maximumInlineAttempts)
	checkpoints := &readCheckpoints{}
	for attemptNumber := int32(1); ; attemptNumber++ {
		call.attempt = attemptNumber
		attempt, runErr := call.runReadAttempt(ctx, binding, stepExecutionID, attemptNumber, checkpoints)
		if errors.Is(runErr, sdkgo.ErrAgentToolArgumentsInvalid) {
			return callAnswer{result: rejectedResult("invalid arguments for " + call.name + ": " + runErr.Error())}
		}
		if runErr != nil {
			call.executor.logger.Warn("attempt ended without a branch", "tool", call.name, "attempt", attemptNumber, "error", runErr)
			return callAnswer{err: interruptedError(binding.operationLabel() + " ended without a result")}
		}
		if attempt.Outcome != sdkgo.AgentToolAttemptRetry {
			return callAnswer{result: branchResult(binding, attempt, call.executor.config.maxResultBytes)}
		}
		detail := retryDetail(attempt)
		if attemptNumber >= limit {
			return callAnswer{result: rejectedResult(fmt.Sprintf("%s still asked to retry after %d attempts (%s)", binding.operationLabel(), attemptNumber, detail))}
		}
		now := call.executor.clock.Now()
		delay := retryDelay(policy, attemptNumber, attempt.RetryAfter)
		if delay+binding.attemptTimeout() > call.remainingBudget(now) {
			return callAnswer{err: interruptedError(binding.operationLabel() + " asked to retry and the call budget is spent")}
		}
		if !call.executor.clock.Sleep(ctx, delay) {
			return callAnswer{err: interruptedError("the call was canceled")}
		}
	}
}

func (call *toolCall) runReadAttempt(ctx context.Context, binding *toolBinding, stepExecutionID string, attemptNumber int32, checkpoints *readCheckpoints) (sdkgo.AgentToolAttempt, error) {
	attemptContext, cancel := context.WithTimeout(ctx, binding.attemptTimeout())
	defer cancel()
	dexContext, err := attemptcontext.New(attemptContext, attemptcontext.Options{
		FlowID: localFlowID, StepExecutionID: stepExecutionID, FirstAttemptAt: call.startedAt,
		Attempt: attemptNumber, LastHeartbeat: checkpoints.last(), OnRecordHeartbeat: checkpoints.record,
	})
	if err != nil {
		return sdkgo.AgentToolAttempt{}, err
	}
	return binding.tool.RunAttempt(dexContext, call.arguments)
}

func (call *toolCall) remainingBudget(now time.Time) time.Duration {
	return call.startedAt.Add(call.executor.config.callTimeBudget).Sub(now)
}

func (call *toolCall) answerAfterPanic() callAnswer {
	if call.knownEffect == sdkgo.AgentToolEffectRead {
		return callAnswer{err: interruptedError("the server failed while handling the call")}
	}
	return callAnswer{result: unknownResult("tool " + call.name + " may have taken effect: the server failed while handling the call; do not repeat it")}
}

// keyPrefix is the first 12 characters of the state key; the raw key is never logged.
func (call *toolCall) keyPrefix() string {
	if len(call.stateKey) < 12 {
		return ""
	}
	return call.stateKey[:12]
}

// logAnswer logs one line per call without arguments, results, or failure text.
func (call *toolCall) logAnswer(answer callAnswer) {
	attributes := []any{"tool", call.name, "effect", string(call.knownEffect), "attempt", call.attempt,
		"durationMs", call.executor.clock.Now().Sub(call.startedAt).Milliseconds(), "replayed", answer.isReplayed}
	if call.binding != nil {
		attributes = append(attributes, "connector", call.binding.connector.ConnectorID, "operation", call.binding.metadata.Operation.OperationID, "connection", call.binding.connectionName)
	}
	if call.stateKey != "" {
		attributes = append(attributes, "key", call.keyPrefix())
	}
	outcome := string(resultSucceeded)
	var rpcErr *jsonrpc.Error
	switch {
	case errors.As(answer.err, &rpcErr) && rpcErr.Code == jsonrpc.CodeInvalidParams:
		outcome = "unknownTool"
	case answer.err != nil:
		outcome = "interrupted"
	case answer.result.IsError:
		outcome = string(resultFailed)
		if value, hasOutcome := answer.result.Meta[metaKeyOutcome].(string); hasOutcome {
			outcome = value
		}
	}
	attributes = append(attributes, "outcome", outcome)
	if branch, hasBranch := answer.result.Meta[metaKeyBranch].(string); hasBranch {
		attributes = append(attributes, "branch", branch)
	}
	call.executor.logger.Info("tool call", attributes...)
}

// keyedCheckpointWriter stores each checkpoint of one attempt durably before
// the attempt continues, only while that attempt is the record's current one.
type keyedCheckpointWriter struct {
	mu       sync.Mutex
	lock     *callstate.KeyLock
	record   *callstate.Record
	attempt  int32
	isClosed bool
}

func (writer *keyedCheckpointWriter) persist(checkpoint json.RawMessage) error {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if writer.isClosed || writer.record.Attempt != writer.attempt || writer.record.Status != callstate.StatusRunning {
		return errors.New("the attempt has ended; its checkpoint is not stored")
	}
	previous := writer.record.Checkpoint
	writer.record.Checkpoint = checkpoint
	if err := writer.lock.Write(writer.record); err != nil {
		writer.record.Checkpoint = previous
		return fmt.Errorf("store checkpoint: %w", err)
	}
	return nil
}

// close waits for a checkpoint write in progress and refuses later ones.
func (writer *keyedCheckpointWriter) close() {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	writer.isClosed = true
}

// readCheckpoints keeps a read's checkpoints in memory between its attempts.
type readCheckpoints struct {
	mu         sync.Mutex
	checkpoint json.RawMessage
}

func (checkpoints *readCheckpoints) record(checkpoint json.RawMessage) error {
	checkpoints.mu.Lock()
	defer checkpoints.mu.Unlock()
	checkpoints.checkpoint = checkpoint
	return nil
}

func (checkpoints *readCheckpoints) last() json.RawMessage {
	checkpoints.mu.Lock()
	defer checkpoints.mu.Unlock()
	return checkpoints.checkpoint
}

// isProvenNotSent reports whether the record's current attempt checkpointed
// notSent with no provider ID. canResend follows a claim and proves nothing.
func isProvenNotSent(record *callstate.Record) bool {
	var marker singledispatch.Marker
	if len(bytes.TrimSpace(record.Checkpoint)) == 0 || json.Unmarshal(record.Checkpoint, &marker) != nil {
		return false
	}
	return string(marker.CallID) == record.CallID && marker.Attempt == record.Attempt &&
		marker.State == singledispatch.StateNotSent && marker.ProviderID == ""
}

func recordLabel(record *callstate.Record) string {
	return record.Request.ConnectorID + " " + record.Request.OperationID
}

func retryDetail(attempt sdkgo.AgentToolAttempt) string {
	if attempt.Failure == nil {
		return "retry"
	}
	return string(attempt.Failure.Kind) + ": " + attempt.Failure.Message
}

// effectRank orders effects from least to most conservative.
func effectRank(effect sdkgo.AgentToolEffect) int {
	switch effect {
	case sdkgo.AgentToolEffectRead:
		return 0
	case sdkgo.AgentToolEffectIdempotentWrite:
		return 1
	default:
		return 2
	}
}

// idempotencyKeyOf returns the request's key, whether it has one, and a
// problem with a malformed one.
func idempotencyKeyOf(meta mcp.Meta) (string, bool, string) {
	value, isPresent := meta[metaKeyIdempotencyKey]
	if !isPresent || value == nil {
		return "", false, ""
	}
	key, isString := value.(string)
	if !isString || callstate.ValidateIdempotencyKey(key) != nil {
		return "", false, fmt.Sprintf("_meta %s must be a non-empty string of at most %d bytes", metaKeyIdempotencyKey, callstate.MaximumIdempotencyKeyBytes)
	}
	return key, true, ""
}

// normalizeArguments treats absent and null arguments as {}.
func normalizeArguments(arguments json.RawMessage) json.RawMessage {
	trimmed := bytes.TrimSpace(arguments)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return json.RawMessage("{}")
	}
	return trimmed
}

// digestArguments is the SHA-256 of the canonical arguments, or of their
// bytes when they are not JSON.
func digestArguments(arguments json.RawMessage) string {
	canonical, err := canonicaljson.Canonicalize(arguments)
	if err != nil {
		canonical = arguments
	}
	return canonicaljson.SHA256Hex(canonical)
}
