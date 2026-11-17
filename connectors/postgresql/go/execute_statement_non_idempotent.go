// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package postgresql

import (
	"context"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

const executeNonIdempotentOperationID = "executeNonIdempotent"

// nonIdempotentWriteWindow leaves the request most of the attempt, because nothing is read back.
const nonIdempotentWriteWindow = 5.0 / 6.0

// ExecuteStatementNonIdempotentOperation implements the transactional write that never runs twice.
type ExecuteStatementNonIdempotentOperation struct{ client *Client }

// Definition returns the immutable connector operation definition.
func (ExecuteStatementNonIdempotentOperation) Definition() sdkgo.MutationDefinition {
	return ExecuteStatementNonIdempotentDefinition
}

// IdempotencyKey returns the stable connector Call ID, a UUID, as PostgreSQL's idempotency key.
func (ExecuteStatementNonIdempotentOperation) IdempotencyKey(callID sdkgo.CallID, _ ExecuteStatementInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke runs one statement that must not run twice in its own transaction and commits it at most once.
//
// Invoke works like ExecuteStatementOperation.Invoke until COMMIT, and every failure before COMMIT
// leaves nothing committed: transient ones return Retry and conclusive ones select a branch. A
// single-dispatch fence, checkpointed in sync Step heartbeats, guards the COMMIT. Once an attempt
// may have sent COMMIT, no later attempt of the Step execution runs the statement again: a COMMIT
// whose outcome is unknown, or a later attempt after such a COMMIT or after a lost Worker, selects
// uncertain. PostgreSQL offers no generic read-back for an arbitrary statement, so the application
// reconciles uncertain itself, for example by reading a row the statement writes.
//
// A COMMIT that PostgreSQL definitely rolled back, such as SQLSTATE 40001, returns Retry.
// An unreadable fence checkpoint returns Retry without connecting.
func (operation ExecuteStatementNonIdempotentOperation) Invoke(call sdkgo.Call, input ExecuteStatementInput) sdkgo.MutationAttempt[StatementExecution] {
	client := operation.client
	statement, err := bindExecuteStatement(call, input)
	if err != nil {
		return writeDefect(executeNonIdempotentOperationID, sdkgo.FailureValidation, err.Error(), sdkgo.Receipt{})
	}
	fence, err := singledispatch.NewFence(call, ExecuteStatementNonIdempotentDefinition.StepDefaults.ExecuteMethodTimeout,
		singledispatch.WithWriteWindow(nonIdempotentWriteWindow), singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return writeDefect(executeNonIdempotentOperationID, sdkgo.FailureLocalDefect, "PostgreSQL dispatch fence could not be constructed", sdkgo.Receipt{})
	}
	decision := fence.Decide()
	switch decision.Kind {
	case singledispatch.SendAllowed:
	case singledispatch.FailClosed:
		return sdkgo.NewMutationRetry[StatementExecution](sdkFailure(executeNonIdempotentOperationID, classifiedFailure{
			kind: sdkgo.FailureAvailability, message: "the dispatch checkpoint could not be read, so nothing was sent in this attempt",
		}), 0)
	default:
		return sdkgo.NewMutationUncertain(emptyStatementExecution(), sdkFailure(executeNonIdempotentOperationID, classifiedFailure{
			kind: sdkgo.FailureTransport, message: "an earlier attempt may have committed the statement, and executeNonIdempotent never runs it again",
		}), client.receipt(call, nil, classifiedFailure{}))
	}
	// Overcounts when an earlier attempt proved its COMMIT unsent, which only costs a rejection branch.
	hasPossibleEarlierSend := decision.MaximumPriorSends > 0
	password, isResolved := client.resolvePassword(call)
	if !isResolved {
		failure := classifiedFailure{disposition: dispositionDefect, kind: sdkgo.FailureAuthentication, message: "PostgreSQL connection credentials are unavailable"}
		return settleBeforeCommit(fence, hasPossibleEarlierSend, failure, sdkgo.Receipt{})
	}
	ctx, cancel := client.newCallContext(call)
	defer cancel()
	connection, err := client.connect(ctx, password)
	if err != nil {
		failure := classifyFailure(err, phaseConnect)
		return settleBeforeCommit(fence, hasPossibleEarlierSend, failure, client.receipt(call, nil, failure))
	}
	defer client.closeConnection(connection)
	read, failure, isReadyToCommit := client.runStatementBeforeCommit(ctx, connection, statement, input.MaxRowsAffected)
	if !isReadyToCommit {
		return settleBeforeCommit(fence, hasPossibleEarlierSend, failure, client.receipt(call, connection, failure))
	}
	callDeadline, _ := ctx.Deadline()
	var commitErr error
	isCommitAttempted := false
	err = fence.Dispatch(func(requestContext context.Context) error {
		isCommitAttempted = true
		commitContext, cancelCommit := context.WithDeadline(requestContext, callDeadline)
		defer cancelCommit()
		commitErr = client.commitTransaction(commitContext, connection)
		return commitErr
	})
	if !isCommitAttempted {
		return sdkgo.NewMutationRetry[StatementExecution](sdkFailure(executeNonIdempotentOperationID, classifiedFailure{
			kind: sdkgo.FailureAvailability, message: "COMMIT was not sent because the dispatch checkpoint failed or too little time remained, so the transaction rolled back",
		}), 0)
	}
	if err != nil {
		failure := classifyFailure(commitErr, phaseCommit)
		receipt := client.receipt(call, connection, failure)
		if failure.disposition == dispositionUncertain {
			return writeAttemptForFailure(executeNonIdempotentOperationID, failure, receipt)
		}
		// PostgreSQL rolled the transaction back; a lost checkpoint only makes a later attempt uncertain.
		_ = fence.RecordCanResend()
		if failure.disposition != dispositionRetry && hasPossibleEarlierSend {
			failure.disposition = dispositionUncertain
		}
		return writeAttemptForFailure(executeNonIdempotentOperationID, failure, receipt)
	}
	return sdkgo.NewMutationBranch(ExecuteStatementNonIdempotentBranchCompleted, newStatementExecution(read), nil, client.receipt(call, connection, classifiedFailure{}))
}

// settleBeforeCommit records that this attempt sent no COMMIT, then reports its failure.
// A refusal after a possible earlier send selects uncertain, since that send may have committed.
func settleBeforeCommit(fence *singledispatch.Fence, hasPossibleEarlierSend bool, failure classifiedFailure, receipt sdkgo.Receipt) sdkgo.MutationAttempt[StatementExecution] {
	// A lost notSent checkpoint only makes the next attempt select uncertain, never send twice.
	_ = fence.RecordNotSent()
	if failure.disposition != dispositionRetry && hasPossibleEarlierSend {
		failure.disposition = dispositionUncertain
	}
	return writeAttemptForFailure(executeNonIdempotentOperationID, failure, receipt)
}
