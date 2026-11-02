// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newMutationBranch, newMutationRetry, newMutationUncertain, newQueryBranch, newQueryRetry, zeroReceipt, type Call, type Failure, type Mutation, type MutationAttempt, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import { newFence } from '@superdurable/connectors-sdk/singledispatch';
import { Client, dataObject, failure, rejection, type Exchange } from './client.js';
import { AddCommentDefinition, CreateTaskDefinition, GetTaskDefinition, ListTasksDefinition, UpdateTaskDefinition } from './generated/definitions.js';
import type { AddCommentInput, AddCommentOutput, CreateTaskInput, CreateTaskOutput, GetTaskInput, ListTasksInput, ListTasksOutput, Task, UpdateTaskInput, UpdateTaskOutput } from './generated/models.js';
import { assignee, createdStoryFields, createdTaskFields, customFieldValues, decodeTask, dueDate, gid, optionalGid, plainText, taskDetailFields, taskName, taskSummaryFields } from './task.js';

const zeroTime = '0001-01-01T00:00:00Z';
const emptyTask: Task = { id: '', name: '', isCompleted: false, createdAt: zeroTime, modifiedAt: zeroTime };
const emptyList: ListTasksOutput = { tasks: null };
const retryableStatus = (status: number) => status === 408 || status === 429 || status >= 500;
const successStatus = (status: number) => status >= 200 && status < 300;
const record = (value: unknown): Record<string, unknown> => value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : {};
const string = (value: unknown): string => typeof value === 'string' ? value : '';
const parseTime = (value: unknown): string | undefined => {
  const raw = string(value);
  if (!raw) return undefined;
  const date = new Date(raw);
  return Number.isNaN(date.getTime()) ? undefined : date.toISOString().replace(/\.000Z$/, 'Z');
};
const queryForFields = (fields: string): URLSearchParams => new URLSearchParams({ opt_fields: fields });

function readOutcome(client: Client, operation: string, subject: string, exchange: Exchange): { kind: 'success' | 'retry' | 'notFound' | 'providerRejected' | 'invalidResponse' | 'defect'; error?: Failure; delay: number } {
  const delay = client.retryAfter(exchange);
  if ('error' in exchange) {
    if (exchange.error === 'notBuilt') return { kind: 'defect', error: failure(operation, 'LOCAL_DEFECT', 'Asana request could not be built'), delay };
    return { kind: 'retry', error: failure(operation, 'TRANSPORT', `Asana ${subject} is temporarily unavailable`), delay };
  }
  const status = exchange.response.status;
  if (status === 404) return { kind: 'notFound', error: failure(operation, 'NOT_FOUND', `Asana ${subject} was not found or is not visible to the connection`), delay };
  if (status === 429) return { kind: 'retry', error: failure(operation, 'RATE_LIMIT', `Asana rate limited the ${subject}`), delay };
  if (retryableStatus(status)) return { kind: 'retry', error: failure(operation, 'AVAILABILITY', `Asana ${subject} is temporarily unavailable`), delay };
  if (status >= 400 && status < 500) return { kind: 'providerRejected', error: rejection(operation, subject, status), delay };
  if (!successStatus(status)) return { kind: 'invalidResponse', error: failure(operation, 'PROTOCOL', `Asana returned unexpected HTTP ${status} for the ${subject}`), delay };
  if (exchange.isTooLarge) return { kind: 'invalidResponse', error: failure(operation, 'RESPONSE_TOO_LARGE', `Asana ${subject} response exceeds the configured size limit`), delay };
  if (exchange.reflectsSecret) return { kind: 'invalidResponse', error: failure(operation, 'PROTOCOL', `Asana ${subject} response contains credential material`), delay };
  return { kind: 'success', delay };
}

function writeOutcome(client: Client, operation: string, subject: string, exchange: Exchange, repeatable: boolean): { kind: 'success' | 'retry' | 'notFound' | 'providerRejected' | 'invalidResponse' | 'defect' | 'uncertain'; error?: Failure; delay: number } {
  const delay = client.retryAfter(exchange);
  if ('error' in exchange) {
    if (exchange.error === 'notBuilt') return { kind: 'defect', error: failure(operation, 'LOCAL_DEFECT', 'Asana request could not be built'), delay };
    if (exchange.error === 'beforeSend' || repeatable) return { kind: 'retry', error: failure(operation, 'TRANSPORT', `Asana ${subject} outcome is unknown, so the update is sent again`), delay };
    return { kind: 'uncertain', error: failure(operation, 'TRANSPORT', `Asana ${subject} outcome is unknown`), delay };
  }
  const status = exchange.response.status;
  if (successStatus(status)) {
    if (exchange.isTooLarge) return { kind: repeatable ? 'invalidResponse' : 'uncertain', error: failure(operation, 'RESPONSE_TOO_LARGE', `Asana ${subject} response exceeds the configured size limit`), delay };
    if (exchange.reflectsSecret) return { kind: repeatable ? 'invalidResponse' : 'uncertain', error: failure(operation, 'PROTOCOL', `Asana ${subject} response contains credential material`), delay };
    return { kind: 'success', delay };
  }
  if (status === 429) return { kind: 'retry', error: failure(operation, 'RATE_LIMIT', `Asana rate limited the ${subject}`), delay };
  if (status === 503 || (repeatable && retryableStatus(status))) return { kind: 'retry', error: failure(operation, 'AVAILABILITY', `Asana ${subject} is temporarily unavailable`), delay };
  if (status === 404) return { kind: 'notFound', error: rejection(operation, subject, status), delay };
  if (status >= 400 && status < 500 && status !== 408) return { kind: 'providerRejected', error: rejection(operation, subject, status), delay };
  if (repeatable) return { kind: 'invalidResponse', error: failure(operation, 'PROTOCOL', `Asana returned unexpected HTTP ${status} for the ${subject}`), delay };
  return { kind: 'uncertain', error: failure(operation, 'AVAILABILITY', `Asana ${subject} outcome is unknown after HTTP ${status}`), delay };
}

/** List one bounded task page from a project or section. */
export class ListTasksOperation implements Query<ListTasksInput, ListTasksOutput> {
  /** Bind a validated Asana client. */ constructor(private readonly client: Client) {}
  /** Return the stable manifest definition. */ definition() { return ListTasksDefinition; }
  /** Read one page and classify one provider attempt. */
  async invoke(call: Call, input: ListTasksInput): Promise<QueryAttempt<ListTasksOutput>> {
    let query: URLSearchParams;
    try { query = listQuery(input); }
    catch (error) { return newQueryBranch('defect', emptyList, zeroReceipt(), failure('listTasks', 'VALIDATION', String(error))); }
    const token = await this.client.token(call);
    if (!token) return newQueryBranch('defect', emptyList, zeroReceipt(), failure('listTasks', 'AUTHENTICATION', 'Asana connection credentials are unavailable'));
    const exchange = await this.client.exchange(call, token, 'GET', '/tasks', query);
    const classified = readOutcome(this.client, 'listTasks', 'task list', exchange);
    if (classified.kind === 'retry') return newQueryRetry(classified.error!, classified.delay);
    const receipt = this.client.receipt(call);
    if (classified.kind !== 'success') return newQueryBranch(classified.kind, emptyList, receipt, classified.error);
    if (!('response' in exchange)) throw new TypeError('successful read requires a response');
    try {
      const page = JSON.parse(Buffer.from(exchange.response.body).toString('utf8')) as Record<string, unknown>;
      if (!Array.isArray(page.data) || page.data.length > 100) throw new TypeError('response has no data array');
      const tasks = page.data.map(value => decodeTask(value, false));
      const nextOffset = string(record(page.next_page).offset);
      if (nextOffset && !validOffset(nextOffset)) throw new TypeError('next page offset is invalid');
      return newQueryBranch('listed', { tasks, ...(nextOffset ? { nextOffset } : {}) }, receipt);
    } catch { return newQueryBranch('invalidResponse', emptyList, receipt, failure('listTasks', 'PROTOCOL', 'Asana returned an invalid task page')); }
  }
}

/** Read one task with notes and custom field values. */
export class GetTaskOperation implements Query<GetTaskInput, Task> {
  /** Bind a validated Asana client. */ constructor(private readonly client: Client) {}
  /** Return the stable manifest definition. */ definition() { return GetTaskDefinition; }
  /** Read one task and classify one provider attempt. */
  async invoke(call: Call, input: GetTaskInput): Promise<QueryAttempt<Task>> {
    let taskId: string;
    try { taskId = gid(input.taskId, 'taskId'); }
    catch (error) { return newQueryBranch('defect', emptyTask, zeroReceipt(), failure('getTask', 'VALIDATION', String(error))); }
    const token = await this.client.token(call);
    if (!token) return newQueryBranch('defect', emptyTask, zeroReceipt(), failure('getTask', 'AUTHENTICATION', 'Asana connection credentials are unavailable'));
    const exchange = await this.client.exchange(call, token, 'GET', `/tasks/${taskId}`, queryForFields(taskDetailFields));
    const classified = readOutcome(this.client, 'getTask', 'task', exchange);
    if (classified.kind === 'retry') return newQueryRetry(classified.error!, classified.delay);
    const receipt = this.client.receipt(call, taskId);
    if (classified.kind !== 'success') return newQueryBranch(classified.kind, emptyTask, receipt, classified.error);
    try { return newQueryBranch('found', decodeTask(dataObject(exchange), true), receipt); }
    catch { return newQueryBranch('invalidResponse', emptyTask, receipt, failure('getTask', 'PROTOCOL', 'Asana returned an invalid task')); }
  }
}

/** Create one Asana task with a checkpointed, non-repeatable POST. */
export class CreateTaskOperation implements Mutation<CreateTaskInput, CreateTaskOutput> {
  /** Bind a validated Asana client. */ constructor(private readonly client: Client) {}
  /** Return the stable manifest definition. */ definition() { return CreateTaskDefinition; }
  /** Use the Call ID in Dex receipts; Asana does not accept this key. */ idempotencyKey(callId: string): string { return callId; }
  /** Create or read back one task without repeating an ambiguous write. */
  async invoke(call: Call, input: CreateTaskInput): Promise<MutationAttempt<CreateTaskOutput>> {
    const requested: CreateTaskOutput = { name: input.name?.trim() ?? '', ...(input.projectId?.trim() ? { projectId: input.projectId.trim() } : {}), ...(input.sectionId?.trim() ? { sectionId: input.sectionId.trim() } : {}), ...(input.workspaceId?.trim() ? { workspaceId: input.workspaceId.trim() } : {}) };
    let fields: Record<string, unknown>;
    try { fields = createFields(input); }
    catch (error) { return newMutationBranch('defect', requested, zeroReceipt(), failure('createTask', 'VALIDATION', String(error))); }
    const fence = newFence(call, 30_000);
    const decision = fence.decide();
    if (decision.kind === 'failClosed') return newMutationUncertain(requested, failure('createTask', 'LOCAL_DEFECT', 'Asana task dispatch checkpoint is unavailable'), this.client.receipt(call));
    const taskId = decision.marker?.providerId;
    if (decision.kind === 'readBackRequired' && !taskId) return newMutationUncertain(requested, failure('createTask', 'TRANSPORT', 'Asana task dispatch checkpoint does not permit another create'), this.client.receipt(call));
    const token = await this.client.token(call);
    if (!token) return taskId ? newMutationRetry(failure('createTask', 'AUTHENTICATION', 'Asana connection credentials are unavailable')) : newMutationBranch('defect', requested, zeroReceipt(), failure('createTask', 'AUTHENTICATION', 'Asana connection credentials are unavailable'));
    if (taskId) return this.readBack(call, token, requested, taskId);
    let exchange: Exchange;
    try { exchange = await fence.dispatch(signal => this.client.exchange(call, token, 'POST', '/tasks', queryForFields(createdTaskFields), { data: fields }, signal)); }
    catch { return newMutationRetry(failure('createTask', 'AVAILABILITY', 'Asana task dispatch checkpoint could not be recorded')); }
    const classified = writeOutcome(this.client, 'createTask', 'task', exchange, false);
    const receipt = this.client.receipt(call);
    if (classified.kind === 'retry') {
      try { await fence.recordCanResend(); } catch { return newMutationUncertain(requested, failure('createTask', 'LOCAL_DEFECT', 'Asana task retry checkpoint is unavailable'), receipt); }
      return newMutationRetry(classified.error!, classified.delay);
    }
    if (classified.kind === 'uncertain') return newMutationUncertain(requested, classified.error!, receipt);
    if (classified.kind !== 'success') return newMutationBranch(classified.kind === 'notFound' ? 'providerRejected' : classified.kind, requested, receipt, classified.error);
    let resource: Record<string, unknown>;
    try { resource = dataObject(exchange); gid(string(resource.gid), 'task gid'); }
    catch { return newMutationUncertain(requested, failure('createTask', 'PROTOCOL', 'Asana accepted the task but returned an unusable task reference'), receipt); }
    const createdId = string(resource.gid);
    try { await fence.recordProviderId(createdId); } catch { /* A lost checkpoint cannot authorize another POST. */ }
    return newMutationBranch('created', { ...requested, taskId: createdId, ...(string(resource.permalink_url) ? { permalinkUrl: string(resource.permalink_url) } : {}), ...(parseTime(resource.created_at) ? { createdAt: parseTime(resource.created_at) } : {}) }, this.client.receipt(call, createdId));
  }
  private async readBack(call: Call, token: string, requested: CreateTaskOutput, taskId: string): Promise<MutationAttempt<CreateTaskOutput>> {
    const exchange = await this.client.exchange(call, token, 'GET', `/tasks/${gid(taskId, 'taskId')}`, queryForFields(createdTaskFields));
    const classified = readOutcome(this.client, 'createTask', 'task', exchange);
    if (classified.kind === 'retry') return newMutationRetry(classified.error!, classified.delay);
    const receipt = this.client.receipt(call, taskId);
    if (classified.kind !== 'success') return newMutationUncertain(requested, failure('createTask', 'PROTOCOL', 'Asana task could not be confirmed by ID'), receipt);
    try {
      const resource = dataObject(exchange);
      if (resource.gid !== taskId) throw new TypeError('task ID differs');
      return newMutationBranch('created', { ...requested, taskId, ...(string(resource.permalink_url) ? { permalinkUrl: string(resource.permalink_url) } : {}), ...(parseTime(resource.created_at) ? { createdAt: parseTime(resource.created_at) } : {}) }, receipt);
    } catch { return newMutationUncertain(requested, failure('createTask', 'PROTOCOL', 'Asana task read-back did not match the checkpointed ID'), receipt); }
  }
}

/** Apply absolute task field and section changes. */
export class UpdateTaskOperation implements Mutation<UpdateTaskInput, UpdateTaskOutput> {
  /** Bind a validated Asana client. */ constructor(private readonly client: Client) {}
  /** Return the stable manifest definition. */ definition() { return UpdateTaskDefinition; }
  /** Use the stable Call ID in Dex receipts. */ idempotencyKey(callId: string): string { return callId; }
  /** Apply repeatable changes and read back the resulting task. */
  async invoke(call: Call, input: UpdateTaskInput): Promise<MutationAttempt<UpdateTaskOutput>> {
    const output: UpdateTaskOutput = { taskId: input.taskId?.trim() ?? '' };
    let fields: Record<string, unknown>, taskId: string, sectionId: string;
    try { ({ fields, taskId, sectionId } = updateFields(input)); }
    catch (error) { return newMutationBranch('defect', output, zeroReceipt(), failure('updateTask', 'VALIDATION', String(error))); }
    const token = await this.client.token(call);
    if (!token) return newMutationBranch('defect', output, zeroReceipt(), failure('updateTask', 'AUTHENTICATION', 'Asana connection credentials are unavailable'));
    const receipt = this.client.receipt(call, taskId);
    if (sectionId) {
      const exchange = await this.client.exchange(call, token, 'POST', `/sections/${sectionId}/addTask`, new URLSearchParams(), { data: { task: taskId } });
      const classified = writeOutcome(this.client, 'updateTask', 'section move', exchange, true);
      const attempt = updateWriteAttempt(classified, output, receipt);
      if (attempt) return attempt;
      output.isMovedToSection = true;
    }
    if (Object.keys(fields).length) {
      const exchange = await this.client.exchange(call, token, 'PUT', `/tasks/${taskId}`, queryForFields(taskDetailFields), { data: fields });
      const classified = writeOutcome(this.client, 'updateTask', 'task update', exchange, true);
      const attempt = updateWriteAttempt(classified, output, receipt);
      if (attempt) return attempt;
      try {
        const task = decodeTask(dataObject(exchange), true);
        if (task.id !== taskId) throw new TypeError('task ID differs');
        return newMutationBranch('updated', { ...output, task }, receipt);
      } catch { return newMutationBranch('invalidResponse', output, receipt, failure('updateTask', 'PROTOCOL', 'Asana returned an invalid task after the update')); }
    }
    const exchange = await this.client.exchange(call, token, 'GET', `/tasks/${taskId}`, queryForFields(taskDetailFields));
    const classified = readOutcome(this.client, 'updateTask', 'task', exchange);
    if (classified.kind === 'retry') return newMutationRetry(classified.error!, classified.delay);
    if (classified.kind !== 'success') return newMutationBranch(classified.kind, output, receipt, classified.error);
    try { return newMutationBranch('updated', { ...output, task: decodeTask(dataObject(exchange), true) }, receipt); }
    catch { return newMutationBranch('invalidResponse', output, receipt, failure('updateTask', 'PROTOCOL', 'Asana returned an invalid task')); }
  }
}

function updateWriteAttempt(classified: ReturnType<typeof writeOutcome>, output: UpdateTaskOutput, receipt: ReturnType<Client['receipt']>): MutationAttempt<UpdateTaskOutput> | undefined {
  if (classified.kind === 'success') return undefined;
  if (classified.kind === 'retry') return newMutationRetry(classified.error!, classified.delay);
  return newMutationBranch(classified.kind === 'uncertain' ? 'invalidResponse' : classified.kind, output, receipt, classified.error);
}

/** Add one comment with a checkpointed, non-repeatable POST. */
export class AddCommentOperation implements Mutation<AddCommentInput, AddCommentOutput> {
  /** Bind a validated Asana client. */ constructor(private readonly client: Client) {}
  /** Return the stable manifest definition. */ definition() { return AddCommentDefinition; }
  /** Use the Call ID in Dex receipts; Asana does not accept this key. */ idempotencyKey(callId: string): string { return callId; }
  /** Add or read back one story without repeating an ambiguous write. */
  async invoke(call: Call, input: AddCommentInput): Promise<MutationAttempt<AddCommentOutput>> {
    const requested: AddCommentOutput = { taskId: input.taskId?.trim() ?? '' };
    let taskId: string;
    try { taskId = gid(input.taskId, 'taskId'); plainText(input.text, 'text', true); }
    catch (error) { return newMutationBranch('defect', requested, zeroReceipt(), failure('addComment', 'VALIDATION', String(error))); }
    const fence = newFence(call, 30_000);
    const decision = fence.decide();
    if (decision.kind === 'failClosed') return newMutationUncertain(requested, failure('addComment', 'LOCAL_DEFECT', 'Asana comment dispatch checkpoint is unavailable'), this.client.receipt(call));
    const storyId = decision.marker?.providerId;
    if (decision.kind === 'readBackRequired' && !storyId) return newMutationUncertain(requested, failure('addComment', 'TRANSPORT', 'Asana comment dispatch checkpoint does not permit another comment'), this.client.receipt(call));
    const token = await this.client.token(call);
    if (!token) return storyId ? newMutationRetry(failure('addComment', 'AUTHENTICATION', 'Asana connection credentials are unavailable')) : newMutationBranch('defect', requested, zeroReceipt(), failure('addComment', 'AUTHENTICATION', 'Asana connection credentials are unavailable'));
    if (storyId) return this.readBack(call, token, requested, storyId);
    let exchange: Exchange;
    try { exchange = await fence.dispatch(signal => this.client.exchange(call, token, 'POST', `/tasks/${taskId}/stories`, queryForFields(createdStoryFields), { data: { text: input.text } }, signal)); }
    catch { return newMutationRetry(failure('addComment', 'AVAILABILITY', 'Asana comment dispatch checkpoint could not be recorded')); }
    const classified = writeOutcome(this.client, 'addComment', 'comment', exchange, false);
    const receipt = this.client.receipt(call);
    if (classified.kind === 'retry') {
      try { await fence.recordCanResend(); } catch { return newMutationUncertain(requested, failure('addComment', 'LOCAL_DEFECT', 'Asana comment retry checkpoint is unavailable'), receipt); }
      return newMutationRetry(classified.error!, classified.delay);
    }
    if (classified.kind === 'uncertain') return newMutationUncertain(requested, classified.error!, receipt);
    if (classified.kind !== 'success') return newMutationBranch(classified.kind, requested, receipt, classified.error);
    let resource: Record<string, unknown>;
    try { resource = dataObject(exchange); gid(string(resource.gid), 'story gid'); }
    catch { return newMutationUncertain(requested, failure('addComment', 'PROTOCOL', 'Asana accepted the comment but returned an unusable story reference'), receipt); }
    const createdId = string(resource.gid);
    try { await fence.recordProviderId(createdId); } catch { /* A lost checkpoint cannot authorize another POST. */ }
    return newMutationBranch('added', storyOutput(requested, resource), this.client.receipt(call, createdId));
  }
  private async readBack(call: Call, token: string, requested: AddCommentOutput, storyId: string): Promise<MutationAttempt<AddCommentOutput>> {
    const exchange = await this.client.exchange(call, token, 'GET', `/stories/${gid(storyId, 'storyId')}`, queryForFields(createdStoryFields));
    const classified = readOutcome(this.client, 'addComment', 'comment', exchange);
    if (classified.kind === 'retry') return newMutationRetry(classified.error!, classified.delay);
    const receipt = this.client.receipt(call, storyId);
    if (classified.kind !== 'success') return newMutationUncertain(requested, failure('addComment', 'PROTOCOL', 'Asana comment could not be confirmed by ID'), receipt);
    try {
      const resource = dataObject(exchange);
      if (resource.gid !== storyId) throw new TypeError('story ID differs');
      return newMutationBranch('added', storyOutput(requested, resource), receipt);
    } catch { return newMutationUncertain(requested, failure('addComment', 'PROTOCOL', 'Asana comment read-back did not match the checkpointed ID'), receipt); }
  }
}

function storyOutput(requested: AddCommentOutput, resource: Record<string, unknown>): AddCommentOutput {
  const author = record(resource.created_by);
  return { ...requested, commentId: string(resource.gid), ...(/^[0-9]{1,64}$/.test(string(author.gid)) ? { author: { id: string(author.gid), ...(string(author.name) ? { name: string(author.name) } : {}) } } : {}), ...(parseTime(resource.created_at) ? { createdAt: parseTime(resource.created_at) } : {}) };
}

function validOffset(value: string): boolean { return Buffer.byteLength(value) <= 4096 && /^[!-~]+$/.test(value); }
function listQuery(input: ListTasksInput): URLSearchParams {
  const projectId = optionalGid(input.projectId, 'projectId');
  const sectionId = optionalGid(input.sectionId, 'sectionId');
  if (!!projectId === !!sectionId) throw new TypeError('set exactly one of projectId and sectionId');
  const query = queryForFields(taskSummaryFields);
  query.set(projectId ? 'project' : 'section', projectId || sectionId);
  if (input.isIncompleteOnly && input.completedSince) throw new TypeError('set at most one of isIncompleteOnly and completedSince');
  if (input.isIncompleteOnly) query.set('completed_since', 'now');
  else if (input.completedSince) query.set('completed_since', new Date(input.completedSince).toISOString().replace(/\.000Z$/, 'Z'));
  if (input.modifiedSince) query.set('modified_since', new Date(input.modifiedSince).toISOString().replace(/\.000Z$/, 'Z'));
  const size = input.pageSize || 50;
  if (!Number.isSafeInteger(size) || size < 1 || size > 100) throw new TypeError('pageSize must be between 1 and 100');
  query.set('limit', String(size));
  const offset = input.offset?.trim() ?? '';
  if (offset) {
    if (!validOffset(offset)) throw new TypeError('offset must be the token from a previous task page');
    query.set('offset', offset);
  }
  return query;
}
function createFields(input: CreateTaskInput): Record<string, unknown> {
  const name = taskName(input.name);
  const projectId = optionalGid(input.projectId, 'projectId');
  const sectionId = optionalGid(input.sectionId, 'sectionId');
  const workspaceId = optionalGid(input.workspaceId, 'workspaceId');
  if (!!projectId === !!workspaceId) throw new TypeError('set exactly one of projectId and workspaceId');
  if (sectionId && !projectId) throw new TypeError('sectionId requires projectId');
  const fields: Record<string, unknown> = { name };
  if (input.notes && plainText(input.notes, 'notes', false).trim()) fields.notes = input.notes;
  if (sectionId) fields.memberships = [{ project: projectId, section: sectionId }];
  else if (projectId) fields.projects = [projectId];
  if (workspaceId) fields.workspace = workspaceId;
  if (input.assigneeId?.trim()) fields.assignee = assignee(input.assigneeId, 'assigneeId');
  if (input.dueOn?.trim()) fields.due_on = dueDate(input.dueOn, 'dueOn');
  const custom = customFieldValues(input.customFields);
  if (custom) fields.custom_fields = custom;
  return fields;
}
function updateFields(input: UpdateTaskInput): { taskId: string; sectionId: string; fields: Record<string, unknown> } {
  const taskId = gid(input.taskId, 'taskId');
  const sectionId = optionalGid(input.sectionId, 'sectionId');
  const fields: Record<string, unknown> = {};
  if (input.isCompleted !== undefined) fields.completed = input.isCompleted;
  if (input.assigneeId !== undefined) fields.assignee = input.assigneeId.trim() ? assignee(input.assigneeId, 'assigneeId') : null;
  if (input.dueOn !== undefined) fields.due_on = input.dueOn.trim() ? dueDate(input.dueOn, 'dueOn') : null;
  const custom = customFieldValues(input.customFields);
  if (custom) fields.custom_fields = custom;
  if (!sectionId && !Object.keys(fields).length) throw new TypeError('set at least one of isCompleted, assigneeId, dueOn, customFields, and sectionId');
  return { taskId, sectionId, fields };
}
