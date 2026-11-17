// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { definitionMaxSends, newMutationBranch, newMutationRetry, newMutationUncertain, newQueryBranch, newQueryRetry, zeroReceipt, type Call, type Failure, type Mutation, type MutationAttempt, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import { CheckpointError, InsufficientTimeError, newFence, type Decision, type Fence } from '@superdurable/connectors-sdk/singledispatch';
import { CreateTaskDefinition, DeleteTaskDefinition, GetTaskDefinition, ListTasksDefinition, UpdateTaskDefinition } from './generated/definitions.js';
import type { CreateTaskInput, DeleteTaskInput, DeleteTaskOutcome, GetTaskInput, ListTasksInput, Task, TaskPage, UpdateTaskInput } from './generated/models.js';
import { createRequestID, decodeTask, decodeTaskPage, definiteWriteRefusalStatuses, emptyPage, emptyTask, isTaskID, isValidCursor, isValidUTF8, todoistFailure, type TodoistAttempt, type TodoistClient, type TodoistExchange } from './client.js';

const zeroDeletion: DeleteTaskOutcome = { taskId: '' };
const requestIDPattern = /^[0-9a-f]{8}-(?:[0-9a-f]{4}-){3}[0-9a-f]{12}$/;

/** Read one active task. */
export class GetTaskOperation implements Query<GetTaskInput, Task> {
  /** Bind a Todoist client. */ constructor(private readonly client: TodoistClient) {}
  /** Return the generated query definition. */ definition() { return GetTaskDefinition; }
  /** Read and classify one provider attempt. */
  async invoke(call: Call, input: GetTaskInput): Promise<QueryAttempt<Task>> {
    const operation = 'getTask';
    if (!isTaskID(input.taskId)) return newQueryBranch('defect', emptyTask, zeroReceipt(), todoistFailure(operation, 'VALIDATION', 'taskId must be a non-empty opaque task ID'));
    const attempt = await this.client.attempt(call, operation);
    const answer = attempt.failure ?? await this.client.taskRequest(call, attempt, operation, 'GET', input.taskId);
    if (!answer.failure) return newQueryBranch('found', answer.task!, zeroReceipt());
    if (answer.isLocalDefect) return newQueryBranch('defect', emptyTask, zeroReceipt(), answer.failure);
    if (answer.isTransient || answer.status === 429) return newQueryRetry(answer.failure, answer.retryAfterMs);
    if (answer.status === 404) return newQueryBranch('notFound', emptyTask, zeroReceipt(), answer.failure);
    if (answer.failure.kind === 'PROTOCOL' || answer.failure.kind === 'RESPONSE_TOO_LARGE') return newQueryBranch('invalidResponse', emptyTask, zeroReceipt(), answer.failure);
    return newQueryBranch('providerRejected', emptyTask, zeroReceipt(), answer.failure);
  }
}

/** Replace specified task fields; confirm unusable successful answers by read-back. */
export class UpdateTaskOperation implements Mutation<UpdateTaskInput, Task> {
  /** Bind a Todoist client. */ constructor(private readonly client: TodoistClient) {}
  /** Return the generated mutation definition. */ definition() { return UpdateTaskDefinition; }
  /** Absolute replacements are safe under the stable call ID. */ idempotencyKey(callID: string): string { return callID; }
  /** Update and classify one provider attempt. */
  async invoke(call: Call, input: UpdateTaskInput): Promise<MutationAttempt<Task>> {
    const operation = 'updateTaskContent';
    if (!isTaskID(input.taskId) || input.content === undefined && input.description === undefined || input.content !== undefined && (!isValidUTF8(input.content) || !trimGoSpace(input.content)) || input.description !== undefined && !isValidUTF8(input.description)) {
      return newMutationBranch('defect', emptyTask, zeroReceipt(), todoistFailure(operation, 'VALIDATION', 'taskId and at least one valid field are required; specified content must be non-blank UTF-8'));
    }
    const payload = Buffer.from(JSON.stringify({ ...(input.content !== undefined ? { content: input.content } : {}), ...(input.description !== undefined ? { description: input.description } : {}) }));
    if (payload.length > 1 << 20) return newMutationBranch('defect', emptyTask, zeroReceipt(), todoistFailure(operation, 'VALIDATION', "encoded fields exceed Todoist's 1 MiB request limit"));
    const attempt = await this.client.attempt(call, operation);
    const answer = attempt.failure ?? await this.client.taskRequest(call, attempt, operation, 'POST', input.taskId, payload, true);
    if (isSuccess(answer.status) && (answer.failure || !matchesUpdate(answer.task!, input))) return this.confirmUpdate(call, attempt, input, answer);
    if (!answer.failure) return newMutationBranch('updated', answer.task!, zeroReceipt());
    if (answer.isLocalDefect) return newMutationBranch('defect', emptyTask, zeroReceipt(), answer.failure);
    if (answer.isTransient || answer.status === 429) return newMutationRetry(answer.failure, answer.retryAfterMs);
    if (answer.status === 404) return newMutationBranch('notFound', emptyTask, zeroReceipt(), answer.failure);
    if (definiteWriteRefusalStatuses.has(answer.status)) return newMutationBranch('providerRejected', emptyTask, zeroReceipt(), answer.failure);
    // Repeating an absolute replacement is safe, so an unknown outcome retries.
    return newMutationRetry(answer.failure, answer.retryAfterMs);
  }
  private async confirmUpdate(call: Call, attempt: TodoistAttempt, input: UpdateTaskInput, answer: TodoistExchange): Promise<MutationAttempt<Task>> {
    const operation = 'updateTaskContent';
    const readBack = await this.client.taskRequest(call, attempt, operation, 'GET', input.taskId);
    if (!readBack.failure && matchesUpdate(readBack.task!, input)) return newMutationBranch('updated', readBack.task!, zeroReceipt());
    if (readBack.isLocalDefect) return newMutationBranch('defect', emptyTask, zeroReceipt(), readBack.failure!);
    if (readBack.status === 404) return newMutationBranch('notFound', emptyTask, zeroReceipt(), todoistFailure(operation, 'NOT_FOUND', 'Todoist answered the update, but the task is no longer active; it may be completed or deleted, so the specified fields are unconfirmed'));
    if (definiteWriteRefusalStatuses.has(readBack.status)) return newMutationBranch('providerRejected', emptyTask, zeroReceipt(), todoistFailure(operation, readBack.failure!.kind, `Todoist rejected the read-back with HTTP ${readBack.status}, so the update could not be confirmed; repeating it is safe because it sets absolute field values`));
    if (readBack.failure) return newMutationRetry(readBack.failure, readBack.retryAfterMs);
    if (answer.failure) return newMutationRetry(answer.failure, answer.retryAfterMs);
    return newMutationRetry(todoistFailure(operation, 'PROTOCOL', 'Todoist did not confirm the specified fields'));
  }
}

/** Read one bounded page of active tasks. */
export class ListTasksOperation implements Query<ListTasksInput, TaskPage> {
  /** Bind a Todoist client. */ constructor(private readonly client: TodoistClient) {}
  /** Return the generated query definition. */ definition() { return ListTasksDefinition; }
  /** Read and validate one cursor page. */
  async invoke(call: Call, input: ListTasksInput): Promise<QueryAttempt<TaskPage>> {
    const operation = 'listTasks';
    if (input.projectId && !isTaskID(input.projectId) || (input.limit ?? 0) < 0 || (input.limit ?? 0) > 200 || !Number.isInteger(input.limit ?? 0) || !isValidCursor(input.cursor ?? '')) {
      return newQueryBranch('defect', emptyPage, zeroReceipt(), todoistFailure(operation, 'VALIDATION', 'invalid projectId, cursor, or limit'));
    }
    const query = new URLSearchParams();
    if (input.cursor) query.set('cursor', input.cursor);
    if (input.limit) query.set('limit', String(input.limit));
    if (input.projectId) query.set('project_id', input.projectId);
    const endpoint = `/tasks${query.size ? `?${query.toString()}` : ''}`;
    const attempt = await this.client.attempt(call, operation);
    const answer = attempt.failure ?? await this.client.request(call, attempt, operation, 'GET', endpoint);
    if (answer.failure) {
      if (answer.isLocalDefect) return newQueryBranch('defect', emptyPage, zeroReceipt(), answer.failure);
      if (answer.isTransient || answer.status === 429) return newQueryRetry(answer.failure, answer.retryAfterMs);
      if (answer.failure.kind === 'PROTOCOL' || answer.failure.kind === 'RESPONSE_TOO_LARGE') return newQueryBranch('invalidResponse', emptyPage, zeroReceipt(), answer.failure);
      return newQueryBranch('providerRejected', emptyPage, zeroReceipt(), answer.failure);
    }
    try { return newQueryBranch('listed', decodeTaskPage(answer.body, input), zeroReceipt()); }
    catch { return newQueryBranch('invalidResponse', emptyPage, zeroReceipt(), todoistFailure(operation, 'PROTOCOL', 'Todoist returned an invalid task page')); }
  }
}

/** Create one task using Todoist's stable request identity. */
export class CreateTaskOperation implements Mutation<CreateTaskInput, Task> {
  /** Bind a Todoist client. */ constructor(private readonly client: TodoistClient) {}
  /** Return the generated mutation definition. */ definition() { return CreateTaskDefinition; }
  /** Hash the Dex Call ID into Todoist's UUID-shaped request ID. */ idempotencyKey(callID: string): string { return createRequestID(callID); }
  /** Create behind the Fence; resend an unknown outcome below the cap, never a known task ID. */
  async invoke(call: Call, input: CreateTaskInput): Promise<MutationAttempt<Task>> {
    const operation = 'createTask';
    if (!isValidUTF8(input.content) || !trimGoSpace(input.content) || !isValidUTF8(input.description ?? '') || input.projectId && !isTaskID(input.projectId) || !requestIDPattern.test(call.idempotencyKey ?? '')) {
      return newMutationBranch('defect', emptyTask, zeroReceipt(), todoistFailure(operation, 'VALIDATION', 'valid content, description, projectId, and stable request identity are required'));
    }
    const payload = Buffer.from(JSON.stringify({ content: input.content, ...(input.description ? { description: input.description } : {}), ...(input.projectId ? { project_id: input.projectId } : {}) }));
    if (payload.length > 1 << 20) return newMutationBranch('defect', emptyTask, zeroReceipt(), todoistFailure(operation, 'VALIDATION', "encoded task exceeds Todoist's 1 MiB request limit"));
    const write = startFencedCreate(call);
    if (!(write instanceof FencedCreate)) return write;
    const attempt = await this.client.attempt(call, operation);
    const acceptedTaskID = write.decision.marker?.providerId;
    if (acceptedTaskID) return this.readAcceptedTask(call, attempt, acceptedTaskID);
    if (attempt.failure) return write.credentialFailure(attempt.failure);
    let answer: TodoistExchange;
    try { answer = await write.fence.dispatch(() => this.client.request(call, attempt, operation, 'POST', '/tasks', payload, call.idempotencyKey!, true)); }
    catch (error) {
      if (error instanceof CheckpointError || error instanceof InsufficientTimeError) return newMutationRetry(todoistFailure(operation, 'AVAILABILITY', 'Todoist dispatch checkpoint or attempt time is unavailable, so the request was not sent'));
      return newMutationRetry(todoistFailure(operation, 'AVAILABILITY', 'Todoist dispatch was not allowed, so nothing was sent'));
    }
    if (answer.failure) return write.attemptForUnacceptedCreate(answer);
    try {
      const task = decodeTask(answer.body);
      await write.recordAcceptedTask(task.id);
      return newMutationBranch('created', task, zeroReceipt());
    } catch { /* A 2xx with only a task ID needs one confirming read. */ }
    let taskID = '';
    try { const parsed: unknown = JSON.parse(Buffer.from(answer.body).toString('utf8')); if (parsed && typeof parsed === 'object' && 'id' in parsed && typeof parsed.id === 'string' && isTaskID(parsed.id)) taskID = parsed.id; }
    catch { /* A malformed 2xx carries no task ID, so its outcome is unknown. */ }
    if (!taskID) return write.resendOrSelectUncertain(todoistFailure(operation, 'PROTOCOL', 'Todoist answered the creation without a usable task'));
    await write.recordAcceptedTask(taskID);
    const readBack = await this.client.taskRequest(call, attempt, operation, 'GET', taskID);
    if (!readBack.failure) return newMutationBranch('created', readBack.task!, zeroReceipt());
    return newMutationUncertain(emptyTask, todoistFailure(operation, 'PROTOCOL', `Todoist accepted the creation of task ${taskID} but it could not be read back in this attempt, so the task is unconfirmed`), zeroReceipt());
  }
  /** Read back the task an earlier attempt saved; never resend it. */
  private async readAcceptedTask(call: Call, attempt: TodoistAttempt, taskID: string): Promise<MutationAttempt<Task>> {
    const operation = 'createTask';
    const readBack = attempt.failure ?? await this.client.taskRequest(call, attempt, operation, 'GET', taskID);
    if (!readBack.failure) return newMutationBranch('created', readBack.task!, zeroReceipt());
    if (!readBack.isLocalDefect && (readBack.isTransient || readBack.status === 429)) return newMutationRetry(readBack.failure, readBack.retryAfterMs);
    return newMutationUncertain(emptyTask, todoistFailure(operation, 'PROTOCOL', `Todoist accepted the creation of task ${taskID} in an earlier attempt but it could not be read back, so the task is unconfirmed`), zeroReceipt());
  }
}

/** One createTask attempt behind the single-dispatch Fence; unknown outcomes resend until the cap. */
class FencedCreate {
  /** True while another send stays within the cap. */ readonly canResendUnknownOutcome: boolean;
  /** True when an earlier send of this call may have taken effect. */ readonly hasPossibleEarlierSend: boolean;
  /** Capture the Fence decision and the effective send cap. */
  constructor(readonly fence: Fence, readonly decision: Decision, maxSends: number) {
    const priorSends = decision.priorSendUpperBound ?? 0;
    this.canResendUnknownOutcome = priorSends + 1 < maxSends;
    this.hasPossibleEarlierSend = priorSends > 0;
  }
  /** Map a credential failure before the send. */
  async credentialFailure(answer: TodoistExchange): Promise<MutationAttempt<Task>> {
    // notSent after a possible earlier send would hide it, so only a first-send decision records it.
    if (this.decision.kind === 'sendAllowed') {
      try { await this.fence.recordNotSent(); } catch { return newMutationRetry(todoistFailure('createTask', 'AVAILABILITY', 'Todoist credentials failed and the not-sent checkpoint could not be recorded')); }
    }
    if (answer.isLocalDefect) return this.defectOrSelectUncertain(answer.failure!);
    return newMutationRetry(answer.failure!, answer.retryAfterMs);
  }
  /** Map a sent request without a usable task; an unknown outcome resends below the cap. */
  async attemptForUnacceptedCreate(answer: TodoistExchange): Promise<MutationAttempt<Task>> {
    const cause = answer.failure!;
    if (answer.isLocalDefect) {
      // The request was never built, so nothing was sent; a lost checkpoint only keeps the claim.
      try { await this.fence.recordCanResend(); } catch { /* The claim stays, which never permits more than the cap. */ }
      return this.defectOrSelectUncertain(cause);
    }
    if (answer.isUndispatched || answer.status === 429 || answer.status === 503) {
      try { await this.fence.recordCanResend(); } catch { return newMutationRetry(todoistFailure('createTask', 'AVAILABILITY', 'Todoist retry checkpoint could not be recorded'), answer.retryAfterMs); }
      return newMutationRetry(cause, answer.retryAfterMs);
    }
    if (definiteWriteRefusalStatuses.has(answer.status)) return this.rejectOrSelectUncertain(cause);
    return this.resendOrSelectUncertain(cause, answer.retryAfterMs);
  }
  /** Save the accepted task ID so a replayed attempt reads it back instead of resending. */
  async recordAcceptedTask(taskID: string): Promise<void> {
    try { await this.fence.recordProviderId(taskID); } catch { /* The accepted result stands; a lost checkpoint keeps the claim. */ }
  }
  /** Retry an unknown outcome while the send cap allows another send. */
  resendOrSelectUncertain(cause: Failure, retryAfterMs = 0): MutationAttempt<Task> {
    // The claim stays the last marker, so the next attempt counts this send.
    return this.canResendUnknownOutcome ? newMutationRetry(cause, retryAfterMs) : newMutationUncertain(emptyTask, cause, zeroReceipt());
  }
  /** Select providerRejected only when no earlier send of this call may have written. */
  async rejectOrSelectUncertain(cause: Failure): Promise<MutationAttempt<Task>> {
    if (this.hasPossibleEarlierSend) return newMutationUncertain(emptyTask, cause, zeroReceipt());
    try { await this.fence.recordCanResend(); } catch { return newMutationRetry(todoistFailure('createTask', 'AVAILABILITY', 'Todoist rejection checkpoint could not be recorded')); }
    return newMutationBranch('providerRejected', emptyTask, zeroReceipt(), cause);
  }
  /** Select defect only when no earlier send of this call may have written. */
  defectOrSelectUncertain(cause: Failure): MutationAttempt<Task> {
    if (this.hasPossibleEarlierSend) return newMutationUncertain(emptyTask, cause, zeroReceipt());
    return newMutationBranch('defect', emptyTask, zeroReceipt(), cause);
  }
}

/** Decide whether this attempt may send, or return the attempt that forbids it. */
function startFencedCreate(call: Call): FencedCreate | MutationAttempt<Task> {
  const operationMaxSends = definitionMaxSends(CreateTaskDefinition);
  let fence: Fence;
  try { fence = newFence(call, 30_000, { maxSends: operationMaxSends }); }
  catch { return newMutationRetry(todoistFailure('createTask', 'AVAILABILITY', 'Todoist dispatch checkpoint is unavailable, so nothing was sent in this attempt')); }
  const decision = fence.decide();
  if (decision.kind === 'failClosed') return newMutationRetry(todoistFailure('createTask', 'AVAILABILITY', 'Todoist dispatch checkpoint is unreadable, so nothing was sent in this attempt'));
  if (decision.kind === 'readBackRequired' && !decision.marker?.providerId) return newMutationUncertain(emptyTask, todoistFailure('createTask', 'TRANSPORT', 'earlier attempts of this Step may have created the task and the send cap is reached, so it is not sent again'), zeroReceipt());
  // The Fence's own rule: a cap of 1 never resends, and otherwise an application override wins.
  const maxSends = operationMaxSends >= 2 ? call.maxSendsOverride ?? operationMaxSends : 1;
  return new FencedCreate(fence, decision, maxSends);
}

/** Delete a task by immutable ID. */
export class DeleteTaskOperation implements Mutation<DeleteTaskInput, DeleteTaskOutcome> {
  /** Bind a Todoist client. */ constructor(private readonly client: TodoistClient) {}
  /** Return the generated mutation definition. */ definition() { return DeleteTaskDefinition; }
  /** Repeated deletion targets the same immutable task. */ idempotencyKey(callID: string): string { return callID; }
  /** Delete or classify one provider attempt. */
  async invoke(call: Call, input: DeleteTaskInput): Promise<MutationAttempt<DeleteTaskOutcome>> {
    const operation = 'deleteTask';
    if (!isTaskID(input.taskId)) return newMutationBranch('defect', zeroDeletion, zeroReceipt(), todoistFailure(operation, 'VALIDATION', 'taskId must be a non-empty opaque task ID'));
    const attempt = await this.client.attempt(call, operation);
    const answer = attempt.failure ?? await this.client.request(call, attempt, operation, 'DELETE', `/tasks/${input.taskId}`);
    const value = { taskId: input.taskId };
    if (!answer.failure) return newMutationBranch('deleted', value, zeroReceipt());
    if (answer.isLocalDefect) return newMutationBranch('defect', zeroDeletion, zeroReceipt(), answer.failure);
    if (answer.isTransient || answer.status === 429) return newMutationRetry(answer.failure, answer.retryAfterMs);
    if (answer.status === 404) return newMutationBranch('alreadyAbsent', value, zeroReceipt(), answer.failure);
    if (definiteWriteRefusalStatuses.has(answer.status)) return newMutationBranch('providerRejected', zeroDeletion, zeroReceipt(), answer.failure);
    // Repeating a delete by immutable ID is safe, so an unknown outcome retries.
    return newMutationRetry(answer.failure, answer.retryAfterMs);
  }
}

function isSuccess(status: number): boolean { return status >= 200 && status < 300; }
function matchesUpdate(task: Task, input: UpdateTaskInput): boolean { return (input.content === undefined || task.content === input.content) && (input.description === undefined || task.description === input.description); }
function trimGoSpace(value: string): string { return value.replace(/^\p{White_Space}+|\p{White_Space}+$/gu, ''); }
