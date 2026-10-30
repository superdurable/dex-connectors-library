// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { FailureSchema, RetryError, type Failure } from './errors.js';
import type { Call } from './credentials.js';
import { validateMutationDefinition, validateQueryDefinition, type Mutation, type MutationDefinition, type Query, type QueryDefinition } from './operation.js';
import { CallIDSchema, ConnectionRefSchema, DefectBranchID, OperationRefSchema, ReceiptSchema, UncertainBranchID, type MutationAttempt, type MutationResult, type QueryAttempt, type QueryResult, type Receipt, type ReceiptDraft } from './types.js';

/** Invoke a query once and validate its provider-safe terminal result. */
export async function runQuery<Input, Output>(operation: Query<Input, Output>, call: Call, input: Input): Promise<QueryResult<Output>> {
  if (!operation) return defectResult<Output>(undefined, call, 'connector query is required');
  const definition = operation.definition();
  try { validateQueryDefinition(definition); } catch (error) { return defectResult(definition, call, `invalid query definition: ${safeErrorMessage(error)}`); }
  const invalidCall = validateCall(call, definition.operation);
  if (invalidCall) return defectResult(definition, call, invalidCall);
  const attempt = await operation.invoke(call, input);
  return resultFromAttempt(definition, call, attempt, 'query');
}

/** Invoke a mutation once with its stable key and validate the classified outcome. */
export async function runMutation<Input, Output>(operation: Mutation<Input, Output>, call: Call, input: Input): Promise<MutationResult<Output>> {
  if (!operation) return defectResult<Output>(undefined, call, 'connector mutation is required');
  const definition = operation.definition();
  try { validateMutationDefinition(definition); } catch (error) { return defectResult(definition, call, `invalid mutation definition: ${safeErrorMessage(error)}`); }
  const invalidCall = validateCall(call, definition.operation);
  if (invalidCall) return defectResult(definition, call, invalidCall);
  const chosenKey = operation.idempotencyKey(call.id, input) || call.id;
  if (!chosenKey.trim()) return defectResult(definition, call, 'mutation returned an invalid idempotency key');
  const mutationCall: Call = { ...call, context: call.context, idempotencyKey: chosenKey };
  Object.defineProperty(mutationCall, 'context', { value: call.context, enumerable: false });
  const attempt = await operation.invoke(mutationCall, input);
  return resultFromAttempt(definition, mutationCall, attempt, 'mutation');
}

function resultFromAttempt<Output>(definition: QueryDefinition | MutationDefinition, call: Call, attempt: QueryAttempt<Output> | MutationAttempt<Output>, operationKind: 'query' | 'mutation'): QueryResult<Output> {
  if (!attempt || typeof attempt !== 'object') return defectResult(definition, call, `${operationKind} returned an invalid attempt`);
  if (attempt.kind === 'retry') {
    const failure = FailureSchema.safeParse(attempt.failure);
    if (!failure.success || !Number.isSafeInteger(attempt.retryAfterMs) || attempt.retryAfterMs < 0) return defectResult(definition, call, `${operationKind} returned an invalid retry attempt`);
    throw new RetryError(failure.data, attempt.retryAfterMs);
  }
  if (attempt.kind === 'branch' || (operationKind === 'mutation' && attempt.kind === 'uncertain')) {
    const branch = attempt.kind === 'uncertain' ? UncertainBranchID : attempt.branch;
    if (!definition.branches.some(declared => declared.id === branch)) {
      return defectResult(definition, call, attempt.kind === 'uncertain' ? 'mutation returned uncertainty without declaring the uncertain branch' : `${operationKind} returned an unknown branch`);
    }
    const failure = 'failure' in attempt && attempt.failure !== undefined ? FailureSchema.safeParse(attempt.failure) : undefined;
    if (failure && !failure.success) return defectResult(definition, call, `${operationKind} returned an invalid failure`);
    if (attempt.kind === 'uncertain' && !failure) return defectResult(definition, call, 'mutation returned an invalid uncertainty failure');
    let receipt: Receipt;
    try { receipt = completeReceipt(attempt.receipt, call); } catch (error) { return defectResult(definition, call, safeErrorMessage(error)); }
    const result: QueryResult<Output> = { branch, value: attempt.value, receipt };
    if (failure?.success) result.failure = failure.data;
    return result;
  }
  return defectResult(definition, call, `${operationKind} returned an invalid attempt`);
}

function completeReceipt(draft: ReceiptDraft, call: Call): Receipt {
  if (draft.callId && draft.callId !== call.id) throw new TypeError('operation returned a receipt for another call');
  if (draft.idempotencyKey && draft.idempotencyKey !== call.idempotencyKey) throw new TypeError('operation returned a receipt with another idempotency key');
  return ReceiptSchema.parse({ ...draft, callId: call.id, ...(call.idempotencyKey ? { idempotencyKey: call.idempotencyKey } : {}), provider: draft.provider || call.operation.connectorId, observedAt: draft.observedAt && draft.observedAt !== '0001-01-01T00:00:00Z' ? draft.observedAt : new Date().toISOString() });
}

function validateCall(call: Call, operation: QueryDefinition['operation']): string | undefined {
  if (!call) return 'connector call is required';
  if (!CallIDSchema.safeParse(call.id).success) return 'call ID must be a UUID';
  if (!ConnectionRefSchema.safeParse(call.connection).success) return 'connection provider and name are required';
  if (!OperationRefSchema.safeParse(call.operation).success || call.operation.connectorId !== operation.connectorId || call.operation.operationId !== operation.operationId) return 'connector call operation does not match the definition';
  return undefined;
}

function defectResult<Output>(definition: QueryDefinition | undefined, call: Call | undefined, message: string): QueryResult<Output> {
  const operation = definition?.operation;
  const failure: Failure = { kind: 'LOCAL_DEFECT', provider: operation?.connectorId || 'connector', operation: operation?.operationId || 'invoke', message };
  const receipt = call && CallIDSchema.safeParse(call.id).success ? completeReceipt({}, call) : undefined;
  return { branch: DefectBranchID, value: undefined as Output, receipt: receipt as Receipt, failure };
}

function safeErrorMessage(error: unknown): string { return error instanceof Error ? error.message : 'invalid value'; }
