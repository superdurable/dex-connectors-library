// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newMutationBranch, newMutationRetry, newMutationUncertain, newQueryBranch, newQueryRetry, zeroReceipt, type Call, type Mutation, type MutationAttempt, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import { CreateTaskDefinition, DeleteTaskDefinition, GetTaskDefinition, ListTasksDefinition, UpdateTaskDefinition } from './generated/definitions.js';
import type { CreateTaskInput, DeleteTaskInput, DeleteTaskOutcome, GetTaskInput, ListTasksInput, Task, TaskPage, UpdateTaskInput } from './generated/models.js';
import { createRequestID, decodeTask, decodeTaskPage, emptyPage, emptyTask, isTaskID, isValidCursor, isValidUTF8, todoistFailure, type TodoistAttempt, type TodoistClient, type TodoistExchange } from './client.js';

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
    return newMutationBranch('providerRejected', emptyTask, zeroReceipt(), answer.failure);
  }
  private async confirmUpdate(call: Call, attempt: TodoistAttempt, input: UpdateTaskInput, answer: TodoistExchange): Promise<MutationAttempt<Task>> {
    const operation = 'updateTaskContent';
    const readBack = await this.client.taskRequest(call, attempt, operation, 'GET', input.taskId);
    if (!readBack.failure && matchesUpdate(readBack.task!, input)) return newMutationBranch('updated', readBack.task!, zeroReceipt());
    if (readBack.isLocalDefect) return newMutationBranch('defect', emptyTask, zeroReceipt(), readBack.failure!);
    if (readBack.status === 404) return newMutationBranch('notFound', emptyTask, zeroReceipt(), todoistFailure(operation, 'NOT_FOUND', 'Todoist answered the update, but the task is no longer active; it may be completed or deleted, so the specified fields are unconfirmed'));
    if (readBack.status >= 300 && !readBack.isTransient && readBack.status !== 429) return newMutationBranch('providerRejected', emptyTask, zeroReceipt(), todoistFailure(operation, readBack.failure!.kind, `Todoist rejected the read-back with HTTP ${readBack.status}, so the update could not be confirmed; repeating it is safe because it sets absolute field values`));
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
  /** Create or classify one request without repeating an uncertain write. */
  async invoke(call: Call, input: CreateTaskInput): Promise<MutationAttempt<Task>> {
    const operation = 'createTask';
    if (!isValidUTF8(input.content) || !trimGoSpace(input.content) || !isValidUTF8(input.description ?? '') || input.projectId && !isTaskID(input.projectId) || !requestIDPattern.test(call.idempotencyKey ?? '')) {
      return newMutationBranch('defect', emptyTask, zeroReceipt(), todoistFailure(operation, 'VALIDATION', 'valid content, description, projectId, and stable request identity are required'));
    }
    const payload = Buffer.from(JSON.stringify({ content: input.content, ...(input.description ? { description: input.description } : {}), ...(input.projectId ? { project_id: input.projectId } : {}) }));
    if (payload.length > 1 << 20) return newMutationBranch('defect', emptyTask, zeroReceipt(), todoistFailure(operation, 'VALIDATION', "encoded task exceeds Todoist's 1 MiB request limit"));
    const attempt = await this.client.attempt(call, operation);
    const answer = attempt.failure ?? await this.client.request(call, attempt, operation, 'POST', '/tasks', payload, call.idempotencyKey!, true);
    if (answer.failure) {
      if (answer.isLocalDefect) return newMutationBranch('defect', emptyTask, zeroReceipt(), answer.failure);
      if (answer.isUndispatched || answer.status === 429 || answer.status === 503) return newMutationRetry(answer.failure, answer.retryAfterMs);
      if (answer.status === 409 || answer.isTransient || answer.failure.kind === 'PROTOCOL' || answer.failure.kind === 'RESPONSE_TOO_LARGE') return newMutationUncertain(emptyTask, answer.failure, zeroReceipt());
      return newMutationBranch('providerRejected', emptyTask, zeroReceipt(), answer.failure);
    }
    try { return newMutationBranch('created', decodeTask(answer.body), zeroReceipt()); }
    catch { /* A 2xx with only a task ID needs one confirming read. */ }
    let taskID = '';
    try { const parsed: unknown = JSON.parse(Buffer.from(answer.body).toString('utf8')); if (parsed && typeof parsed === 'object' && 'id' in parsed && typeof parsed.id === 'string' && isTaskID(parsed.id)) taskID = parsed.id; }
    catch { /* Malformed successful answers cannot be safely replayed. */ }
    if (!taskID) return newMutationUncertain(emptyTask, todoistFailure(operation, 'PROTOCOL', 'Todoist answered the creation without a usable task'), zeroReceipt());
    const readBack = await this.client.taskRequest(call, attempt, operation, 'GET', taskID);
    if (!readBack.failure) return newMutationBranch('created', readBack.task!, zeroReceipt());
    return newMutationUncertain(emptyTask, todoistFailure(operation, 'PROTOCOL', `Todoist accepted the creation of task ${taskID} but it could not be read back in this attempt, so the task is unconfirmed`), zeroReceipt());
  }
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
    return newMutationBranch('providerRejected', zeroDeletion, zeroReceipt(), answer.failure);
  }
}

function isSuccess(status: number): boolean { return status >= 200 && status < 300; }
function matchesUpdate(task: Task, input: UpdateTaskInput): boolean { return (input.content === undefined || task.content === input.content) && (input.description === undefined || task.description === input.description); }
function trimGoSpace(value: string): string { return value.replace(/^\p{White_Space}+|\p{White_Space}+$/gu, ''); }
