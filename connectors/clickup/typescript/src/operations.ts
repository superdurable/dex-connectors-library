// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newMutationBranch, newMutationRetry, newMutationUncertain, newQueryBranch, newQueryRetry, type Call, type Failure, type Mutation, type MutationAttempt, type Query, type QueryAttempt, type ReceiptDraft } from '@superdurable/connectors-sdk';
import type { ClickUpClient, Exchange } from './client.js';
import { AddCommentDefinition, CreateTaskDefinition, FindMemberByEmailDefinition, GetTaskDefinition, SearchTasksDefinition, UpdateTaskDefinition, UpdateTaskTagsDefinition } from './generated/definitions.js';
import type { AddCommentInput, AddCommentOutput, CreateTaskInput, CreateTaskOutput, FindMemberByEmailInput, GetTaskInput, Member, SearchTasksInput, SearchTasksOutput, Task, UpdateTaskInput, UpdateTaskOutput, UpdateTaskTagsInput, UpdateTaskTagsOutput } from './generated/models.js';

const taskIdPattern = /^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$/;
const numericIdPattern = /^[0-9]{1,20}$/;
const zeroTask: Task = { id: '', name: '', status: { name: '' }, assignees: [], tags: [] };
const zeroMember: Member = { id: 0, email: '', workspaceId: '' };

type Outcome = Extract<Exchange, { failure: Failure }>;
function failure(kind: Failure['kind'], operation: string, message: string): Failure { return { kind, provider: 'clickup', operation, message }; }
function isRetryable(exchange: Exchange): boolean { return exchange.kind === 'notSent' || exchange.kind === 'rateLimited' || exchange.kind === 'unconfirmed'; }
function queryExchange<T>(exchange: Outcome, output: T, receipt: ReceiptDraft): QueryAttempt<T> {
  if (isRetryable(exchange)) return newQueryRetry(exchange.failure, exchange.retryAfterMs);
  const branch = exchange.kind === 'notFound' ? 'notFound' : exchange.kind === 'invalid' ? 'invalidResponse' : exchange.kind === 'defect' ? 'defect' : 'providerRejected';
  return newQueryBranch(branch, output, receipt, exchange.failure);
}
function mutationExchange<T>(exchange: Outcome, output: T, receipt: ReceiptDraft, hasInvalidResponse = true): MutationAttempt<T> {
  if (isRetryable(exchange)) return newMutationRetry(exchange.failure, exchange.retryAfterMs);
  const branch = exchange.kind === 'notFound' ? 'notFound' : exchange.kind === 'invalid' && hasInvalidResponse ? 'invalidResponse' : exchange.kind === 'defect' ? 'defect' : 'providerRejected';
  return newMutationBranch(branch, output, receipt, exchange.failure);
}
function defect<T>(operation: string, output: T, message: string): QueryAttempt<T> {
  const cause = failure('VALIDATION', operation, message);
  return newQueryBranch('defect', output, {}, cause);
}
function decodeObject(body: Uint8Array): Record<string, unknown> {
  const decoded: unknown = JSON.parse(Buffer.from(body).toString('utf8'));
  if (!decoded || typeof decoded !== 'object' || Array.isArray(decoded)) throw new TypeError('not an object');
  return decoded as Record<string, unknown>;
}
function object(value: unknown): Record<string, unknown> { return value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : {}; }
function identifier(value: unknown): string { return typeof value === 'string' ? value : typeof value === 'number' && Number.isSafeInteger(value) ? String(value) : ''; }
function milliseconds(value: unknown): string | undefined {
  if (value === undefined || value === null || value === '') return undefined;
  const numeric = Number(value);
  if (!Number.isSafeInteger(numeric) || numeric < 0) throw new TypeError('invalid Unix milliseconds');
  return numeric > 0 ? new Date(numeric).toISOString().replace(/\.000Z$/, 'Z') : undefined;
}
function taskFromWire(wire: unknown, shouldIncludeDetails: boolean): Task {
  const data = object(wire);
  if (typeof data.id !== 'string' || (data.name !== undefined && typeof data.name !== 'string') ||
      (data.status !== undefined && (typeof data.status !== 'object' || data.status === null || Array.isArray(data.status))) ||
      (data.assignees !== undefined && data.assignees !== null && !Array.isArray(data.assignees)) ||
      (data.tags !== undefined && data.tags !== null && !Array.isArray(data.tags))) throw new TypeError('invalid task fields');
  const id = data.id;
  if (!taskIdPattern.test(id)) throw new TypeError('task has no valid ID');
  const status = object(data.status);
  const priorityWire = data.priority;
  const priorityObject = object(priorityWire);
  const priority = priorityWire === undefined || priorityWire === null ? 0 : Number(typeof priorityWire === 'object' ? priorityObject.id : priorityWire);
  if (priority !== 0 && (![1, 2, 3, 4].includes(priority) || !Number.isInteger(priority))) throw new TypeError('invalid priority');
  const assignees = Array.isArray(data.assignees) ? data.assignees.map(value => { const user = object(value); const userId = Number(user.id); if (!Number.isSafeInteger(userId) || userId <= 0) throw new TypeError('invalid assignee'); return { id: userId, ...(typeof user.username === 'string' && user.username ? { username: user.username } : {}) }; }) : [];
  const tags = Array.isArray(data.tags) ? data.tags.map(value => String(object(value).name ?? '')) : [];
  const result: Task = { id, name: String(data.name ?? ''), status: { name: String(status.status ?? ''), ...(status.type ? { type: String(status.type) } : {}) }, assignees, tags,
    ...(data.url ? { url: String(data.url) } : {}), ...(priority ? { priority } : {}), ...(priorityObject.priority ? { priorityName: String(priorityObject.priority) } : {}),
    ...(milliseconds(data.due_date) ? { dueAt: milliseconds(data.due_date) } : {}), ...(milliseconds(data.start_date) ? { startAt: milliseconds(data.start_date) } : {}),
    ...(milliseconds(data.date_created) ? { createdAt: milliseconds(data.date_created) } : {}), ...(milliseconds(data.date_updated) ? { updatedAt: milliseconds(data.date_updated) } : {}),
    ...(milliseconds(data.date_closed) ? { closedAt: milliseconds(data.date_closed) } : {}), ...(data.archived ? { isArchived: true } : {}),
    ...(identifier(data.team_id) ? { workspaceId: identifier(data.team_id) } : {}), ...(identifier(object(data.list).id) ? { listId: identifier(object(data.list).id) } : {}),
    ...(object(data.list).name ? { listName: String(object(data.list).name) } : {}), ...(identifier(object(data.folder).id) ? { folderId: identifier(object(data.folder).id) } : {}),
    ...(identifier(object(data.space).id) ? { spaceId: identifier(object(data.space).id) } : {}),
    ...(data.custom_id !== undefined && data.custom_id !== null ? { customId: String(data.custom_id) } : {}),
    ...(data.parent !== undefined && data.parent !== null ? { parentTaskId: String(data.parent) } : {}) };
  const creator = object(data.creator);
  if (Number(creator.id) > 0) result.creator = { id: Number(creator.id), ...(creator.username ? { username: String(creator.username) } : {}) };
  if (shouldIncludeDetails) {
    const description = typeof data.markdown_description === 'string' ? data.markdown_description : typeof data.description === 'string' ? data.description : undefined;
    if (description !== undefined) {
      const bytes = Buffer.from(description);
      const isTruncated = bytes.length > 65_536;
      result.markdownDescription = isTruncated ? new TextDecoder('utf-8', { fatal: false }).decode(bytes.subarray(0, 65_536)).replace(/\uFFFD$/, '') : description;
      if (isTruncated) result.isDescriptionTruncated = true;
    }
    if (data.custom_fields !== undefined && data.custom_fields !== null && !Array.isArray(data.custom_fields)) throw new TypeError('invalid custom fields');
    if (Array.isArray(data.custom_fields)) result.customFields = data.custom_fields.slice(0, 100).map(value => {
      const field = object(value); const fieldId = identifier(field.id);
      if (!taskIdPattern.test(fieldId)) throw new TypeError('invalid custom field ID');
      return { id: fieldId, name: String(field.name ?? ''), type: String(field.type ?? ''), ...(field.value === undefined || field.value === null ? {} : { value: field.value }) };
    });
  }
  return result;
}
function taskBody(body: Uint8Array, shouldIncludeDetails: boolean): Task { return taskFromWire(decodeObject(body), shouldIncludeDetails); }
function searchPage(body: Uint8Array, page: number): SearchTasksOutput {
  const data = decodeObject(body);
  if (!Array.isArray(data.tasks) || data.tasks.length > 100 || (data.last_page !== undefined && data.last_page !== null && typeof data.last_page !== 'boolean')) throw new TypeError('invalid tasks array');
  const tasks = data.tasks.map(value => taskFromWire(value, false));
  const hasMore = typeof data.last_page === 'boolean' ? !data.last_page && tasks.length > 0 : tasks.length === 100;
  return { tasks, page, ...(hasMore ? { nextPage: page + 1 } : {}) };
}
function validateTaskId(id: string): void { if (!taskIdPattern.test(id)) throw new TypeError('taskId must be a ClickUp task ID'); }
function validateNumericId(id: string): void { if (!numericIdPattern.test(id)) throw new TypeError('ID must be numeric'); }
function dateMilliseconds(value: string | null | undefined): number | undefined { if (!value) return undefined; if (!/^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d+)?(?:Z|[+-]\d\d:\d\d)$/.test(value)) throw new TypeError('date must be RFC 3339'); const ms = Date.parse(value); if (!Number.isSafeInteger(ms) || ms <= 0) throw new TypeError('date must be after the Unix epoch'); return ms; }
function validatePriority(value: number | undefined): void { if (value && ![1, 2, 3, 4].includes(value)) throw new TypeError('priority must be 1 through 4'); }
function hasDuplicate<Value>(values: Value[] | undefined): boolean { return values !== undefined && new Set(values).size !== values.length; }
function isValidUnicode(value: string): boolean { return Buffer.from(value).toString('utf8') === value; }
function validateUserIds(values: number[] | undefined): void { if ((values?.length ?? 0) > 50 || values?.some(value => !Number.isSafeInteger(value) || value <= 0) || hasDuplicate(values)) throw new TypeError('invalid user ID'); }
function validateTags(values: string[] | undefined): void {
  if (values?.some(tag => !tag || tag.trim() !== tag || Buffer.byteLength(tag) > 100 || !isValidUnicode(tag) || tag.includes('/') || /[\0\r\n\t]/.test(tag) || tag === '.' || tag === '..') || hasDuplicate(values)) throw new TypeError('invalid tag name');
}
function validateStatusName(status: string | undefined): void { if (status && (!status.trim() || Buffer.byteLength(status) > 255 || !isValidUnicode(status))) throw new TypeError('invalid status name'); }
function isJSONValue(value: unknown, seen = new Set<object>()): boolean {
  if (value === null || typeof value === 'boolean') return true;
  if (typeof value === 'string') return isValidUnicode(value);
  if (typeof value === 'number') return Number.isFinite(value);
  if (typeof value !== 'object' || seen.has(value)) return false;
  seen.add(value);
  const valid = Array.isArray(value) ? value.every(item => isJSONValue(item, seen)) : Object.getPrototypeOf(value) === Object.prototype && Object.entries(value).every(([key, item]) => isValidUnicode(key) && isJSONValue(item, seen));
  seen.delete(value);
  return valid;
}
function validateName(name: string): void { if (!name.trim() || Buffer.byteLength(name) > 1024) throw new TypeError('invalid task name'); }
function receipt(client: ClickUpClient, call: Call, objectId: string): ReceiptDraft { return client.receipt(call, objectId); }
function pathEscape(value: string): string { return encodeURIComponent(value).replace(/[!'()*]/g, character => `%${character.charCodeAt(0).toString(16).toUpperCase()}`).replace(/%(?:24|26|2B|3A|3D|40)/g, escape => String.fromCharCode(Number.parseInt(escape.slice(1), 16))); }
function path(id: string): string { return `/task/${pathEscape(id)}`; }
function failedDecode<T>(operation: string, output: T, responseReceipt: ReceiptDraft): QueryAttempt<T> {
  const cause = failure('PROTOCOL', operation, `ClickUp returned an invalid ${operation === 'searchTasks' ? 'page of tasks' : operation === 'findMemberByEmail' ? 'list of Workspaces' : 'task'}`);
  return newQueryBranch('invalidResponse', output, responseReceipt, cause);
}

/** Search one Workspace task page. */
export class SearchTasksOperation implements Query<SearchTasksInput, SearchTasksOutput> {
  /** Bind a client. */ constructor(private readonly client: ClickUpClient) {}
  /** Return the generated definition. */ definition() { return SearchTasksDefinition; }
  /** Validate filters, search, and decode the page. */
  async invoke(call: Call, input: SearchTasksInput): Promise<QueryAttempt<SearchTasksOutput>> {
    const output: SearchTasksOutput = { tasks: [], page: input.page ?? 0 };
    try { validateNumericId(input.workspaceId); for (const ids of [input.listIds, input.folderIds, input.spaceIds]) { if ((ids?.length ?? 0) > 50) throw new TypeError('too many IDs'); ids?.forEach(validateNumericId); }
      if ([input.statuses, input.assigneeIds, input.tags].some(values => (values?.length ?? 0) > 50)) throw new TypeError('too many filters');
      validateUserIds(input.assigneeIds); validateTags(input.tags); input.statuses?.forEach(validateStatusName);
      if (input.orderBy && !['created', 'updated', 'due_date', 'id'].includes(input.orderBy)) throw new TypeError('invalid orderBy');
      if (!Number.isInteger(output.page) || output.page < 0 || output.page > 1000) throw new TypeError('invalid page'); dateMilliseconds(input.updatedAfter);
    } catch (error) { return defect('searchTasks', output, String(error)); }
    const query = new URLSearchParams({ page: String(output.page) });
    for (const [name, values] of [['list_ids[]', input.listIds], ['project_ids[]', input.folderIds], ['space_ids[]', input.spaceIds], ['statuses[]', input.statuses], ['assignees[]', input.assigneeIds?.map(String)], ['tags[]', input.tags]] as const) values?.forEach(value => query.append(name, value));
    if (input.updatedAfter) query.set('date_updated_gt', String(dateMilliseconds(input.updatedAfter)));
    if (input.isClosedIncluded) query.set('include_closed', 'true'); if (input.areSubtasksIncluded) query.set('subtasks', 'true');
    if (input.orderBy) query.set('order_by', input.orderBy); if (input.isReverse) query.set('reverse', 'true');
    const result = await this.client.exchange(call, 'searchTasks', 'GET', `/team/${input.workspaceId}/task`, query);
    const responseReceipt = receipt(this.client, call, input.workspaceId);
    if (result.kind !== 'succeeded') return queryExchange(result, output, responseReceipt);
    try { return newQueryBranch('searched', searchPage(result.response.body, output.page), responseReceipt); }
    catch { return failedDecode('searchTasks', output, responseReceipt); }
  }
}

/** Read one task with details. */
export class GetTaskOperation implements Query<GetTaskInput, Task> {
  /** Bind a client. */ constructor(private readonly client: ClickUpClient) {}
  /** Return the generated definition. */ definition() { return GetTaskDefinition; }
  /** Validate, read, and decode one task. */
  async invoke(call: Call, input: GetTaskInput): Promise<QueryAttempt<Task>> {
    try { validateTaskId(input.taskId); if (input.isCustomTaskId) validateNumericId(input.workspaceId ?? ''); else if (input.workspaceId) throw new TypeError('workspaceId requires isCustomTaskId'); }
    catch (error) { return defect('getTask', zeroTask, String(error)); }
    const query = new URLSearchParams({ include_markdown_description: 'true' });
    if (input.isCustomTaskId) { query.set('custom_task_ids', 'true'); query.set('team_id', input.workspaceId!); }
    const result = await this.client.exchange(call, 'getTask', 'GET', path(input.taskId), query);
    const responseReceipt = receipt(this.client, call, input.taskId);
    if (result.kind !== 'succeeded') return queryExchange(result, zeroTask, responseReceipt);
    try { const task = taskBody(result.response.body, true); return newQueryBranch('found', task, receipt(this.client, call, task.id)); }
    catch { return failedDecode('getTask', zeroTask, responseReceipt); }
  }
}

/** Find one member by email in an authorized Workspace. */
export class FindMemberByEmailOperation implements Query<FindMemberByEmailInput, Member> {
  /** Bind a client. */ constructor(private readonly client: ClickUpClient) {}
  /** Return the generated definition. */ definition() { return FindMemberByEmailDefinition; }
  /** Read Workspace members and compare email addresses. */
  async invoke(call: Call, input: FindMemberByEmailInput): Promise<QueryAttempt<Member>> {
    try { validateNumericId(input.workspaceId); if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(input.email)) throw new TypeError('email must be a bare address'); }
    catch (error) { return defect('findMemberByEmail', zeroMember, String(error)); }
    const result = await this.client.exchange(call, 'findMemberByEmail', 'GET', '/team');
    const responseReceipt = receipt(this.client, call, input.workspaceId);
    if (result.kind !== 'succeeded') return result.kind === 'notFound'
      ? newQueryBranch('providerRejected', zeroMember, responseReceipt, result.failure)
      : queryExchange(result, zeroMember, responseReceipt);
    try { const data = decodeObject(result.response.body); if (!Array.isArray(data.teams)) throw new TypeError('missing Workspaces');
      for (const value of data.teams) { const team = object(value); if (identifier(team.id) !== input.workspaceId) continue;
        if (team.members !== undefined && team.members !== null && !Array.isArray(team.members)) throw new TypeError('invalid members');
        for (const entry of Array.isArray(team.members) ? team.members : []) { const member = object(object(entry).user);
          if (String(member.email ?? '').trim().toLowerCase() !== input.email.toLowerCase()) continue;
          const id = Number(member.id); if (!Number.isSafeInteger(id) || id <= 0) throw new TypeError('invalid member');
          const resultMember: Member = { id, email: String(member.email).trim(), workspaceId: input.workspaceId, ...(member.username ? { username: String(member.username) } : {}), ...(member.role ? { role: Number(member.role) } : {}) };
          return newQueryBranch('found', resultMember, receipt(this.client, call, String(id)));
        }
        return newQueryBranch('notFound', zeroMember, responseReceipt, failure('NOT_FOUND', 'findMemberByEmail', 'no member of the Workspace has the email address'));
      }
      return newQueryBranch('notFound', zeroMember, responseReceipt, failure('NOT_FOUND', 'findMemberByEmail', "the token's user is not a member of the Workspace"));
    } catch { return failedDecode('findMemberByEmail', zeroMember, responseReceipt); }
  }
}

/** Apply repeat safe task field changes. */
export class UpdateTaskOperation implements Mutation<UpdateTaskInput, UpdateTaskOutput> {
  /** Bind a client. */ constructor(private readonly client: ClickUpClient) {}
  /** Return the generated definition. */ definition() { return UpdateTaskDefinition; }
  /** Correlate this mutation with the Dex Call ID. */ idempotencyKey(callId: string): string { return callId; }
  /** Validate, update, and decode the task. */
  async invoke(call: Call, input: UpdateTaskInput): Promise<MutationAttempt<UpdateTaskOutput>> {
    const output = { task: zeroTask };
    try { validateTaskId(input.taskId); if (input.name === undefined && input.markdownDescription === undefined && !input.status && !input.priority && !input.dueAt && !(input.addAssigneeIds?.length || input.removeAssigneeIds?.length)) throw new TypeError('no changes');
      if (input.name !== undefined) validateName(input.name); if (input.markdownDescription !== undefined && Buffer.byteLength(input.markdownDescription) > 65_536) throw new TypeError('description too long');
      validateStatusName(input.status); validatePriority(input.priority); dateMilliseconds(input.dueAt); validateUserIds(input.addAssigneeIds); validateUserIds(input.removeAssigneeIds);
      if ((input.addAssigneeIds?.length ?? 0) + (input.removeAssigneeIds?.length ?? 0) > 50 || input.addAssigneeIds?.some(id => input.removeAssigneeIds?.includes(id))) throw new TypeError('invalid assignees');
    } catch (error) { return defect('updateTask', output, String(error)); }
    const payload: Record<string, unknown> = {};
    if (input.name !== undefined) payload.name = input.name;
    if (input.markdownDescription !== undefined) payload[input.markdownDescription ? 'markdown_content' : 'description'] = input.markdownDescription || ' ';
    if (input.status) payload.status = input.status; if (input.priority) payload.priority = input.priority;
    if (input.dueAt) { payload.due_date = dateMilliseconds(input.dueAt); payload.due_date_time = true; }
    if (input.addAssigneeIds?.length || input.removeAssigneeIds?.length) payload.assignees = { add: input.addAssigneeIds ?? [], rem: input.removeAssigneeIds ?? [] };
    const result = await this.client.exchange(call, 'updateTask', 'PUT', path(input.taskId), undefined, payload);
    const responseReceipt = receipt(this.client, call, input.taskId);
    if (result.kind !== 'succeeded') return mutationExchange(result, output, responseReceipt);
    try { return newMutationBranch('updated', { task: taskBody(result.response.body, false) }, responseReceipt); }
    catch { return failedDecode('updateTask', output, responseReceipt); }
  }
}

/** Add and remove tags, then read the resulting task. */
export class UpdateTaskTagsOperation implements Mutation<UpdateTaskTagsInput, UpdateTaskTagsOutput> {
  /** Bind a client. */ constructor(private readonly client: ClickUpClient) {}
  /** Return the generated definition. */ definition() { return UpdateTaskTagsDefinition; }
  /** Correlate this mutation with the Dex Call ID. */ idempotencyKey(callId: string): string { return callId; }
  /** Apply repeat safe tag membership changes. */
  async invoke(call: Call, input: UpdateTaskTagsInput): Promise<MutationAttempt<UpdateTaskTagsOutput>> {
    const output: UpdateTaskTagsOutput = { taskId: input.taskId, tags: [], appliedChanges: 0 };
    try { validateTaskId(input.taskId); const tags = [...(input.addTags ?? []), ...(input.removeTags ?? [])]; if (!tags.length || tags.length > 10) throw new TypeError('invalid tag changes'); validateTags(tags); }
    catch (error) { return defect('updateTaskTags', output, String(error)); }
    const changes = [...(input.addTags ?? []).map(tag => ({ method: 'POST', tag })), ...(input.removeTags ?? []).map(tag => ({ method: 'DELETE', tag }))];
    const responseReceipt = receipt(this.client, call, input.taskId);
    const operationSignal = AbortSignal.any([call.context.cancellationSignal, AbortSignal.timeout(25_000)]);
    for (const change of changes) { const result = await this.client.exchange(call, 'updateTaskTags', change.method, `${path(input.taskId)}/tag/${pathEscape(change.tag)}`, undefined, undefined, operationSignal);
      if (result.kind === 'succeeded' || result.kind === 'invalid') { output.appliedChanges++; continue; }
      return mutationExchange(result, output, responseReceipt);
    }
    const result = await this.client.exchange(call, 'updateTaskTags', 'GET', path(input.taskId), undefined, undefined, operationSignal);
    if (result.kind !== 'succeeded') return mutationExchange(result, output, responseReceipt);
    try { output.tags = taskBody(result.response.body, false).tags; return newMutationBranch('updated', output, responseReceipt); }
    catch { return failedDecode('updateTaskTags', output, responseReceipt); }
  }
}

interface WriteDispatchMarker { clickupDispatchedCallId: string; clickupDispatchedAt: number }
function previousDispatch(call: Call): WriteDispatchMarker | undefined {
  if (!call.context.hasLastHeartbeatValue()) return undefined;
  const value: unknown = call.context.getLastHeartbeatValue();
  if (value === null) return undefined;
  if (!value || typeof value !== 'object') return { clickupDispatchedCallId: call.id, clickupDispatchedAt: 0 };
  const marker = value as Record<string, unknown>;
  return { clickupDispatchedCallId: String(marker.clickupDispatchedCallId ?? ''), clickupDispatchedAt: Number(marker.clickupDispatchedAt ?? 0) };
}
async function clearDispatch(call: Call): Promise<void> { try { await call.context.recordHeartbeat(undefined); } catch { /* An uncleared marker keeps the next attempt in read back. */ } }
function matchText(value: string): string { return value.trim().replace(/\s+/g, ' '); }

/** Create one task with a Dex checkpoint and read back on an ambiguous result. */
export class CreateTaskOperation implements Mutation<CreateTaskInput, CreateTaskOutput> {
  /** Bind a client. */ constructor(private readonly client: ClickUpClient) {}
  /** Return the generated definition. */ definition() { return CreateTaskDefinition; }
  /** Correlate the checkpoint with the Dex Call ID. */ idempotencyKey(callId: string): string { return callId; }
  /** Create or reconcile one task. */
  async invoke(call: Call, input: CreateTaskInput): Promise<MutationAttempt<CreateTaskOutput>> {
    const output: CreateTaskOutput = { listId: input.listId, name: input.name, task: zeroTask };
    try { validateNumericId(input.listId); validateName(input.name); if (Buffer.byteLength(input.markdownDescription ?? '') > 65_536) throw new TypeError('description too long');
      validateStatusName(input.status); validatePriority(input.priority); dateMilliseconds(input.dueAt); validateUserIds(input.assigneeIds); validateTags(input.tags);
      if ((input.tags?.length ?? 0) > 20 || (input.customFields?.length ?? 0) > 50) throw new TypeError('too many values');
      if (input.parentTaskId) validateTaskId(input.parentTaskId);
      if (input.customFields?.some(field => !taskIdPattern.test(field.id) || !isJSONValue(field.value)) || hasDuplicate(input.customFields?.map(field => field.id))) throw new TypeError('invalid custom field');
    } catch (error) { return defect('createTask', output, String(error)); }
    const prior = previousDispatch(call);
    if (prior) return this.reconcile(call, input, output, prior);
    try { await call.context.recordHeartbeat({ clickupDispatchedCallId: call.id, clickupDispatchedAt: this.client.currentTime().getTime() } satisfies WriteDispatchMarker); }
    catch { return newMutationRetry(failure('AVAILABILITY', 'createTask', 'Dex did not record the dispatch checkpoint; nothing was sent')); }
    const payload: Record<string, unknown> = { name: input.name };
    if (input.isCreatorNotified) payload.notify_all = true;
    if (input.markdownDescription) payload.markdown_content = input.markdownDescription;
    if (input.status) payload.status = input.status; if (input.priority) payload.priority = input.priority;
    if (input.dueAt) { payload.due_date = dateMilliseconds(input.dueAt); payload.due_date_time = true; }
    if (input.assigneeIds?.length) payload.assignees = input.assigneeIds;
    if (input.tags?.length) payload.tags = input.tags;
    if (input.parentTaskId) payload.parent = input.parentTaskId;
    if (input.customFields?.length) payload.custom_fields = input.customFields.map(field => ({ id: field.id, value: field.value }));
    const result = await this.client.exchange(call, 'createTask', 'POST', `/list/${input.listId}/task`, undefined, payload);
    const responseReceipt = receipt(this.client, call, input.listId);
    if (result.kind !== 'succeeded') {
      if (result.kind === 'notSent' || result.kind === 'rateLimited') { await clearDispatch(call); return newMutationRetry(result.failure, result.retryAfterMs); }
      if (result.kind === 'unconfirmed' || result.kind === 'invalid') return newMutationRetry(result.failure, result.retryAfterMs);
      if (result.kind === 'defect') await clearDispatch(call);
      return newMutationBranch(result.kind === 'notFound' ? 'notFound' : result.kind === 'defect' ? 'defect' : 'providerRejected', output, responseReceipt, result.failure);
    }
    try { const task = taskBody(result.response.body, false); return newMutationBranch('created', { ...output, task }, receipt(this.client, call, task.id)); }
    catch { return newMutationRetry(failure('PROTOCOL', 'createTask', 'ClickUp accepted the task but returned an invalid task, so the next attempt reads the List back')); }
  }

  private async reconcile(call: Call, input: CreateTaskInput, output: CreateTaskOutput, marker: WriteDispatchMarker): Promise<MutationAttempt<CreateTaskOutput>> {
    const since = marker.clickupDispatchedAt > 0 ? marker.clickupDispatchedAt - 300_000 : 0;
    const query = new URLSearchParams({ include_closed: 'true', subtasks: 'true', page: '0' });
    if (since > 0) query.set('date_created_gt', String(since));
    const result = await this.client.exchange(call, 'createTask', 'GET', `/list/${input.listId}/task`, query);
    const responseReceipt = receipt(this.client, call, input.listId);
    if (result.kind !== 'succeeded') {
      if (isRetryable(result)) return newMutationRetry(result.failure, result.retryAfterMs);
      if (result.kind === 'notFound') return newMutationBranch('notFound', output, responseReceipt, result.failure);
      return newMutationUncertain(output, failure(result.failure.kind, 'createTask', 'an earlier attempt of this Step sent the create, and the List could not be read back: ' + result.failure.message), responseReceipt);
    }
    let matches: Task[];
    try { matches = (searchPage(result.response.body, 0).tasks ?? []).filter(task => task.name.trim() === input.name.trim() && (task.parentTaskId ?? '') === (input.parentTaskId ?? '') && (!since || (task.createdAt !== undefined && Date.parse(task.createdAt) >= since))); }
    catch { return newMutationUncertain(output, failure('PROTOCOL', 'createTask', 'an earlier attempt of this Step sent the create, and the List read back is invalid'), responseReceipt); }
    if (matches.length !== 1) return newMutationUncertain(output, failure('TRANSPORT', 'createTask', `an earlier attempt of this Step sent the create without a confirmed outcome, and the List shows ${matches.length} tasks with the name created since, so it is not sent again`), responseReceipt);
    const task = matches[0]!;
    return newMutationBranch('created', { ...output, task, wasAlreadyApplied: true }, receipt(this.client, call, task.id));
  }
}

/** Add one comment with a Dex checkpoint and read back on an ambiguous result. */
export class AddCommentOperation implements Mutation<AddCommentInput, AddCommentOutput> {
  /** Bind a client. */ constructor(private readonly client: ClickUpClient) {}
  /** Return the generated definition. */ definition() { return AddCommentDefinition; }
  /** Correlate the checkpoint with the Dex Call ID. */ idempotencyKey(callId: string): string { return callId; }
  /** Add or reconcile one comment. */
  async invoke(call: Call, input: AddCommentInput): Promise<MutationAttempt<AddCommentOutput>> {
    const output: AddCommentOutput = { taskId: input.taskId };
    try { validateTaskId(input.taskId); if (!input.text.trim() || Buffer.byteLength(input.text) > 32_768) throw new TypeError('invalid comment text'); }
    catch (error) { return defect('addComment', output, String(error)); }
    const prior = previousDispatch(call);
    if (prior) return this.reconcile(call, input, output, prior);
    try { await call.context.recordHeartbeat({ clickupDispatchedCallId: call.id, clickupDispatchedAt: this.client.currentTime().getTime() } satisfies WriteDispatchMarker); }
    catch { return newMutationRetry(failure('AVAILABILITY', 'addComment', 'Dex did not record the dispatch checkpoint; nothing was sent')); }
    const result = await this.client.exchange(call, 'addComment', 'POST', `${path(input.taskId)}/comment`, undefined, { comment_text: input.text, notify_all: input.isCreatorNotified ?? false });
    const responseReceipt = receipt(this.client, call, input.taskId);
    if (result.kind !== 'succeeded') {
      if (result.kind === 'notSent' || result.kind === 'rateLimited') { await clearDispatch(call); return newMutationRetry(result.failure, result.retryAfterMs); }
      if (result.kind === 'unconfirmed' || result.kind === 'invalid') return newMutationRetry(result.failure, result.retryAfterMs);
      if (result.kind === 'defect') await clearDispatch(call);
      return newMutationBranch(result.kind === 'notFound' ? 'notFound' : result.kind === 'defect' ? 'defect' : 'providerRejected', output, responseReceipt, result.failure);
    }
    try { const data = decodeObject(result.response.body); const id = identifier(data.id); if (!taskIdPattern.test(id)) throw new TypeError('invalid comment ID');
      const createdAt = milliseconds(data.date);
      return newMutationBranch('added', { ...output, commentId: id, ...(createdAt ? { createdAt } : {}) }, { ...responseReceipt, metadata: { commentId: id } });
    } catch { return newMutationRetry(failure('PROTOCOL', 'addComment', 'ClickUp accepted the comment but returned no valid comment ID, so the next attempt reads the comments back')); }
  }

  private async reconcile(call: Call, input: AddCommentInput, output: AddCommentOutput, marker: WriteDispatchMarker): Promise<MutationAttempt<AddCommentOutput>> {
    const result = await this.client.exchange(call, 'addComment', 'GET', `${path(input.taskId)}/comment`);
    const responseReceipt = receipt(this.client, call, input.taskId);
    if (result.kind !== 'succeeded') {
      if (isRetryable(result)) return newMutationRetry(result.failure, result.retryAfterMs);
      if (result.kind === 'notFound') return newMutationBranch('notFound', output, responseReceipt, result.failure);
      return newMutationUncertain(output, failure(result.failure.kind, 'addComment', "an earlier attempt of this Step sent the comment, and the task's comments could not be read back: " + result.failure.message), responseReceipt);
    }
    let matches: { id: string; createdAt?: string | undefined }[];
    try { const data = decodeObject(result.response.body); if (!Array.isArray(data.comments)) throw new TypeError('missing comments');
      const since = marker.clickupDispatchedAt > 0 ? marker.clickupDispatchedAt - 300_000 : 0;
      matches = data.comments.map(value => object(value)).filter(value => matchText(String(value.comment_text ?? '')) === matchText(input.text) && (!since || (milliseconds(value.date) !== undefined && Date.parse(milliseconds(value.date)!) >= since))).map(value => ({ id: identifier(value.id), ...(milliseconds(value.date) ? { createdAt: milliseconds(value.date) } : {}) }));
    } catch { return newMutationUncertain(output, failure('PROTOCOL', 'addComment', 'an earlier attempt of this Step sent the comment, and the comments read back are invalid'), responseReceipt); }
    if (matches.length !== 1 || !taskIdPattern.test(matches[0]!.id)) return newMutationUncertain(output, failure('TRANSPORT', 'addComment', `an earlier attempt of this Step sent the comment without a confirmed outcome, and the task shows ${matches.length} matching comments since, so it is not sent again`), responseReceipt);
    const match = matches[0]!;
    return newMutationBranch('added', { ...output, commentId: match.id, ...(match.createdAt ? { createdAt: match.createdAt } : {}), wasAlreadyApplied: true }, { ...responseReceipt, metadata: { commentId: match.id } });
  }
}
