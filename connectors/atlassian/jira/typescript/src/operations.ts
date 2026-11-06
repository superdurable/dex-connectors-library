// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT
/* eslint-disable no-control-regex -- Jira validates control characters in issue and JQL text. */

import { createHash } from 'node:crypto';
import { newMutationBranch, newMutationRetry, newMutationUncertain, newQueryBranch, newQueryRetry, type Call, type Failure, type Mutation, type MutationAttempt, type Query, type QueryAttempt, type ReceiptDraft } from '@superdurable/connectors-sdk';
import { newFence } from '@superdurable/connectors-sdk/singledispatch';
import { jiraFailure, pathEscape, retryAfter, statusFailureKind, JiraSessionFailure, type JiraClient, type JiraExchange, type JiraSession } from './client.js';
import { AddCommentDefinition, CreateIssueDefinition, GetIssueDefinition, SearchIssuesDefinition, TransitionIssueDefinition } from './generated/definitions.js';
import type { AddCommentInput, AddCommentOutput, CreateIssueInput, CreateIssueOutput, GetIssueInput, Issue, IssueSearchFilter, IssueStatus, SearchIssuesInput, SearchIssuesOutput, TransitionIssueInput, TransitionIssueOutput, TransitionReference } from './generated/models.js';

const numericId = /^[1-9][0-9]{0,17}$/;
const issueKey = /^[A-Za-z][A-Za-z0-9_]*-[1-9][0-9]{0,17}$/;
const projectKey = /^[A-Z][A-Z0-9_]{0,254}$/;
const accountId = /^[A-Za-z0-9:_-]{1,128}$/;
const fieldId = /^[A-Za-z][A-Za-z0-9_.-]{0,63}$/;
const zeroTime = '0001-01-01T00:00:00Z';
const zeroStatus: IssueStatus = { id: '', name: '' };
const zeroIssue: Issue = { id: '', key: '', summary: '', status: zeroStatus, issueType: { id: '', name: '' }, project: { id: '', key: '' }, createdAt: zeroTime, updatedAt: zeroTime };
const zeroTransition: TransitionIssueOutput = { issueId: '', issueKey: '', previousStatus: zeroStatus, status: zeroStatus };
const standardFields = ['summary', 'status', 'issuetype', 'project', 'assignee', 'reporter', 'priority', 'labels', 'created', 'updated'];
const propertyKey = 'dex.connector.call';

type ReadResult = { kind: 'ok'; response: Exclude<JiraExchange, {error: Error}>['response']; receipt: ReceiptDraft } | { kind: 'retry'; failure: Failure; retryAfterMs: number } | { kind: 'branch'; branch: string; failure: Failure; receipt: ReceiptDraft };

/** Query one bounded Jira enhanced-search page. */
export class SearchIssuesOperation implements Query<SearchIssuesInput, SearchIssuesOutput> {
  /** Bind a Jira client. */ constructor(private readonly client: JiraClient) {}
  /** Return the manifest definition. */ definition() { return SearchIssuesDefinition; }
  /** Validate, send and decode one search page. */
  async invoke(call: Call, input: SearchIssuesInput): Promise<QueryAttempt<SearchIssuesOutput>> {
    let body: { jql: string; fields: string[]; maxResults: number; nextPageToken?: string };
    try { body = buildSearchBody(input); }
    catch (error) { return newQueryBranch('defect', { jql: '', issues: [] }, {}, jiraFailure('searchIssues', 'VALIDATION', message(error))); }
    const output: SearchIssuesOutput = { jql: body.jql, issues: [] };
    let session: JiraSession;
    try { session = await this.client.startSession(call, 'searchIssues'); }
    catch (error) { return querySessionFailure(error, output, 'searchIssues'); }
    const exchange = await this.client.exchange(session, 'POST', '/search/jql', undefined, body);
    const read = classifyRead(this.client, session, 'searchIssues', 'issue search', exchange);
    if (read.kind === 'retry') return newQueryRetry(read.failure, read.retryAfterMs);
    if (read.kind === 'branch') return newQueryBranch(read.branch === 'defect' ? 'defect' : read.branch === 'invalidResponse' ? 'invalidResponse' : 'providerRejected', output, read.receipt, read.failure);
    try {
      const page = decodeObject(read.response.body);
      const issuesValue = page.issues;
      if (issuesValue !== undefined && issuesValue !== null && !Array.isArray(issuesValue)) throw new TypeError('issues must be a list');
      const nextPageToken = optionalString(page.nextPageToken);
      if (Buffer.byteLength(nextPageToken, 'utf8') > 4096) throw new TypeError('page token too long');
      const issues = (issuesValue ?? []).map((item: unknown) => decodeIssue(item, input.additionalFields ?? [], false));
      return newQueryBranch('searched', { jql: body.jql, issues, ...(nextPageToken ? { nextPageToken } : {}) }, read.receipt);
    } catch { return newQueryBranch('invalidResponse', output, read.receipt, jiraFailure('searchIssues', 'PROTOCOL', 'Jira returned an invalid search page')); }
  }
}

/** Query one issue and its requested raw fields. */
export class GetIssueOperation implements Query<GetIssueInput, Issue> {
  /** Bind a Jira client. */ constructor(private readonly client: JiraClient) {}
  /** Return the manifest definition. */ definition() { return GetIssueDefinition; }
  /** Validate, send and decode one issue. */
  async invoke(call: Call, input: GetIssueInput): Promise<QueryAttempt<Issue>> {
    let issueIdOrKey: string; let fields: string[];
    try { issueIdOrKey = validateIssueIdOrKey(input.issueIdOrKey); fields = validateFields(input.additionalFields ?? [], true); }
    catch (error) { return newQueryBranch('defect', zeroIssue, {}, jiraFailure('getIssue', 'VALIDATION', message(error))); }
    let session: JiraSession;
    try { session = await this.client.startSession(call, 'getIssue'); }
    catch (error) { return querySessionFailure(error, zeroIssue, 'getIssue'); }
    const exchange = await this.client.exchange(session, 'GET', `/issue/${pathEscape(issueIdOrKey)}`, { fields: fields.join(',') });
    const read = classifyRead(this.client, session, 'getIssue', 'issue', exchange, issueIdOrKey);
    if (read.kind === 'retry') return newQueryRetry(read.failure, read.retryAfterMs);
    if (read.kind === 'branch') return newQueryBranch(read.branch === 'rejected' ? 'providerRejected' : read.branch, zeroIssue, read.receipt, read.failure);
    try {
      const issue = decodeIssue(decodeObject(read.response.body), input.additionalFields ?? [], true);
      return newQueryBranch('found', issue, { ...read.receipt, providerObjectId: issue.key });
    } catch { return newQueryBranch('invalidResponse', zeroIssue, read.receipt, jiraFailure('getIssue', 'PROTOCOL', 'Jira returned an invalid issue')); }
  }
}

function classifyRead(client: JiraClient, session: JiraSession, operation: string, subject: string, exchange: JiraExchange, objectId = ''): ReadResult {
  if ('error' in exchange) return { kind: 'retry', failure: jiraFailure(operation, 'TRANSPORT', `Jira ${subject} is temporarily unavailable`), retryAfterMs: 0 };
  const { response } = exchange;
  const receipt = client.receipt(session, response, objectId);
  if (response.status === 404) return { kind: 'branch', branch: 'notFound', failure: jiraFailure(operation, 'NOT_FOUND', `Jira ${subject} was not found or is not visible to the connection`), receipt };
  if (response.status === 429) return { kind: 'retry', failure: jiraFailure(operation, 'RATE_LIMIT', `Jira rate limited the ${subject}`), retryAfterMs: retryAfter(response) };
  if (response.status === 408 || response.status >= 500) return { kind: 'retry', failure: jiraFailure(operation, 'AVAILABILITY', `Jira ${subject} is temporarily unavailable`), retryAfterMs: retryAfter(response) };
  if (response.status >= 400 && response.status < 500) return { kind: 'branch', branch: 'rejected', failure: rejection(operation, subject, response.status, rejectedFields(response.body)), receipt };
  if (response.status < 200 || response.status >= 300) return { kind: 'branch', branch: 'invalidResponse', failure: jiraFailure(operation, 'PROTOCOL', `Jira returned unexpected HTTP ${response.status} for the ${subject}`), receipt };
  if (exchange.isTooLarge) return { kind: 'branch', branch: 'invalidResponse', failure: jiraFailure(operation, 'RESPONSE_TOO_LARGE', `Jira ${subject} response exceeds the configured size limit`), receipt };
  if (exchange.reflectsSecret) return { kind: 'branch', branch: 'invalidResponse', failure: jiraFailure(operation, 'PROTOCOL', `Jira ${subject} response contains credential material`), receipt };
  return { kind: 'ok', response, receipt };
}

function querySessionFailure<T>(error: unknown, zero: T, operation: string): QueryAttempt<T> {
  if (error instanceof JiraSessionFailure) return error.retry ? newQueryRetry(error.failure, error.retryAfterMs) : newQueryBranch('defect', zero, {}, error.failure);
  return newQueryRetry(jiraFailure(operation, 'AVAILABILITY', 'Jira setup is temporarily unavailable'));
}
function mutationSessionFailure<T>(error: unknown, zero: T, operation: string): MutationAttempt<T> {
  if (error instanceof JiraSessionFailure) return error.retry ? newMutationRetry(error.failure, error.retryAfterMs) : newMutationBranch('defect', zero, {}, error.failure);
  return newMutationRetry(jiraFailure(operation, 'AVAILABILITY', 'Jira setup is temporarily unavailable'));
}
function rejection(operation: string, subject: string, status: number, fields: string[]): Failure { return jiraFailure(operation, statusFailureKind(status), `Jira rejected the ${subject} with HTTP ${status}${fields.length ? ` (fields: ${fields.join(', ')})` : ''}`); }
function rejectedFields(body: Uint8Array): string[] {
  try { const root = decodeObject(body); const errors = root.errors; return errors && typeof errors === 'object' && !Array.isArray(errors) ? Object.keys(errors).filter(key => fieldId.test(key)).sort().slice(0, 20) : []; }
  catch { return []; }
}
function decodeObject(body: Uint8Array): Record<string, unknown> {
  const json = JSON as typeof JSON & { rawJSON(text: string): unknown };
  return requireObject(JSON.parse(Buffer.from(body).toString('utf8'), (_key, value: unknown, context?: { source?: string }) =>
    typeof value === 'number' && context?.source ? json.rawJSON(context.source) : value));
}
function requireObject(value: unknown): Record<string, unknown> { if (!value || typeof value !== 'object' || Array.isArray(value)) throw new TypeError('expected an object'); return value as Record<string, unknown>; }
function requireString(value: unknown): string { if (typeof value !== 'string') throw new TypeError('expected a string'); return value; }
function optionalString(value: unknown): string { return value === undefined || value === null ? '' : requireString(value); }
function message(error: unknown): string { return error instanceof Error ? error.message : 'Jira input is invalid'; }
function validateIssueIdOrKey(value: string): string { const trimmed = value.trim(); if (!numericId.test(trimmed) && !issueKey.test(trimmed)) throw new TypeError('issueIdOrKey must be an issue key or numeric issue ID'); return trimmed; }
function validateFields(additional: string[], description: boolean): string[] { if (additional.length > 20) throw new TypeError('additionalFields accepts at most 20 field IDs'); const fields = [...standardFields, ...(description ? ['description'] : [])]; for (const field of additional) { if (!fieldId.test(field)) throw new TypeError('additionalFields must be Jira field IDs'); if (!fields.includes(field)) fields.push(field); } return fields; }
function parseJiraTime(value: unknown): string {
  if (value === '' || value === undefined || value === null) return zeroTime;
  const text = requireString(value);
  const parts = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:([.,])(\d+))?(Z|([+-])(\d{2}):?(\d{2}))$/.exec(text);
  if (!parts) throw new TypeError('timestamp is not a Jira date-time');
  const [, yearText, monthText, dayText, hourText, minuteText, secondText, separator, fractionText, zone, signText, zoneHourText, zoneMinuteText] = parts;
  const year = Number(yearText), month = Number(monthText), day = Number(dayText), hour = Number(hourText), minute = Number(minuteText), second = Number(secondText);
  const leap = year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0);
  const days = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
  if (month < 1 || month > 12 || day < 1 || day > days[month - 1]! || hour > 23 || minute > 59 || second > 59) throw new TypeError('timestamp is not a Jira date-time');
  if (separator && zone !== 'Z' && !zone?.includes(':') && (separator !== '.' || fractionText?.length !== 3)) throw new TypeError('timestamp is not a Jira date-time');
  const zoneHour = Number(zoneHourText ?? 0), zoneMinute = Number(zoneMinuteText ?? 0);
  if (zoneHour > 99 || zoneMinute > 59) throw new TypeError('timestamp is not a Jira date-time');
  const local = new Date(0); local.setUTCFullYear(year, month - 1, day); local.setUTCHours(hour, minute, second, 0);
  const offset = zone === 'Z' ? 0 : (signText === '+' ? 1 : -1) * (zoneHour * 60 + zoneMinute) * 60_000;
  const utc = new Date(local.getTime() - offset);
  if (!Number.isFinite(utc.getTime())) throw new TypeError('timestamp is not a Jira date-time');
  const fraction = (fractionText ?? '').slice(0, 9).replace(/0+$/, '');
  return utc.toISOString().slice(0, 19) + (fraction ? '.' + fraction : '') + 'Z';
}
function decodeStatus(value: unknown): IssueStatus { const status = requireObject(value); const category = status.statusCategory === undefined || status.statusCategory === null ? undefined : requireObject(status.statusCategory); const categoryKey = category ? optionalString(category.key) : ''; return { id: requireString(status.id ?? ''), name: requireString(status.name ?? ''), ...(categoryKey ? { categoryKey } : {}) }; }
function decodeIssue(value: unknown, additional: string[], description: boolean): Issue {
  const resource = requireObject(value), id = requireString(resource.id), key = requireString(resource.key);
  if (!numericId.test(id) || !issueKey.test(key)) throw new TypeError('issue ID or key is invalid');
  const fields = resource.fields == null ? {} : requireObject(resource.fields);
  const status = fields.status == null ? zeroStatus : decodeStatus(fields.status);
  const kind = fields.issuetype == null ? { id: '', name: '' } : requireObject(fields.issuetype);
  const project = fields.project == null ? { id: '', key: '' } : requireObject(fields.project);
  if (kind.subtask != null && typeof kind.subtask !== 'boolean') throw new TypeError('issue type subtask is invalid');
  const projectName = optionalString(project.name);
  const issue: Issue = { id, key, summary: optionalString(fields.summary), status, issueType: { id: requireString(kind.id ?? ''), name: requireString(kind.name ?? ''), ...(kind.subtask === true ? { isSubtask: true } : {}) }, project: { id: requireString(project.id ?? ''), key: requireString(project.key ?? ''), ...(projectName ? { name: projectName } : {}) }, createdAt: parseJiraTime(fields.created), updatedAt: parseJiraTime(fields.updated) };
  if (fields.labels != null) { if (!Array.isArray(fields.labels) || !fields.labels.every(label => typeof label === 'string')) throw new TypeError('labels are invalid'); if (fields.labels.length) issue.labels = fields.labels; }
  for (const keyName of ['assignee', 'reporter'] as const) if (fields[keyName] != null) { const account = requireObject(fields[keyName]); if (account.accountId) issue[keyName] = { accountId: requireString(account.accountId), ...(account.displayName ? { displayName: requireString(account.displayName) } : {}) }; }
  if (fields.priority != null) { const priority = requireObject(fields.priority); issue.priority = { id: requireString(priority.id ?? ''), name: requireString(priority.name ?? '') }; }
  if (description) { const extracted = extractText(fields.description); if (extracted.text) issue.description = extracted.text; if (extracted.truncated) issue.isDescriptionTruncated = true; }
  for (const field of additional) if (Object.prototype.hasOwnProperty.call(fields, field)) (issue.additionalFields ??= {})[field] = fields[field] as unknown;
  return issue;
}
function extractText(value: unknown): { text: string; truncated: boolean } {
  if (value == null) return { text: '', truncated: false };
  if (typeof value === 'string') return trimRunes(value);
  const document = requireObject(value), chunks: string[] = [];
  validateDocumentNode(document);
  const visit = (node: unknown, depth: number): void => { if (depth > 48) return; const item = requireObject(node), kind = optionalString(item.type); if (kind === 'text') chunks.push(optionalString(item.text)); else if (kind === 'hardBreak') chunks.push('\n'); else if (['mention', 'emoji', 'status'].includes(kind)) chunks.push(optionalString(requireObject(item.attrs ?? {}).text)); else if (['inlineCard', 'blockCard', 'embedCard'].includes(kind)) chunks.push(optionalString(requireObject(item.attrs ?? {}).url)); else if (kind === 'rule') chunks.push('---'); else if (!['media', 'mediaSingle', 'mediaGroup', 'mediaInline'].includes(kind)) { const content = item.content; if (Array.isArray(content)) content.forEach((child, index) => { if (index && ['bulletList', 'orderedList', 'tableRow'].includes(kind)) chunks.push(kind === 'tableRow' ? ' | ' : '\n'); visit(child, depth + 1); }); } };
  if (Array.isArray(document.content)) document.content.forEach((node, index) => { if (index) chunks.push('\n\n'); visit(node, 1); });
  return trimRunes(chunks.join('').replace(/\n+$/, ''));
}
function validateDocumentNode(value: Record<string, unknown>): void {
  for (const name of ['type', 'text']) if (value[name] != null && typeof value[name] !== 'string') throw new TypeError('document is not Atlassian Document Format');
  if (value.version != null && !/^-?(0|[1-9][0-9]*)$/.test(JSON.stringify(value.version))) throw new TypeError('document is not Atlassian Document Format');
  if (value.attrs != null) requireObject(value.attrs);
  if (value.content != null) {
    if (!Array.isArray(value.content)) throw new TypeError('document is not Atlassian Document Format');
    for (const child of value.content) validateDocumentNode(requireObject(child));
  }
}
function trimRunes(value: string): { text: string; truncated: boolean } { const runes = Array.from(value); return { text: runes.slice(0, 32767).join(''), truncated: runes.length > 32767 }; }
function quoteJql(value: string): string { if (!isValidUnicode(value) || [...value].length > 1024 || /[\x00-\x1f\x7f]/.test(value)) throw new TypeError('JQL value is invalid'); return '"' + value.replace(/\\/g, '\\\\').replace(/"/g, '\\"') + '"'; }
function buildSearchBody(input: SearchIssuesInput): { jql: string; fields: string[]; maxResults: number; nextPageToken?: string } {
  let jql: string;
  if (input.filter && input.jql?.trim()) throw new TypeError('set either filter or jql, not both');
  if (input.filter) jql = filterJql(input.filter); else { jql = (input.jql ?? '').trim(); if (!jql || !isValidUnicode(jql) || [...jql].length > 16384 || jql.includes('\0')) throw new TypeError('jql is invalid'); }
  const fields = validateFields(input.additionalFields ?? [], false);
  const maxResults = input.pageSize || 50; if (!Number.isInteger(maxResults) || maxResults < 1 || maxResults > 100) throw new TypeError('pageSize must be between 1 and 100');
  const token = (input.nextPageToken ?? '').trim(); if (Buffer.byteLength(token, 'utf8') > 4096 || (token && !/^[!-~]+$/.test(token))) throw new TypeError('nextPageToken is invalid');
  return { jql, fields, maxResults, ...(token ? { nextPageToken: token } : {}) };
}
function filterJql(filter: IssueSearchFilter): string {
  const clauses: string[] = [];
  for (const [field, values] of [['project', filter.projectKeys], ['issuetype', filter.issueTypeNames], ['status', filter.statusNames], ['labels', filter.labels]] as const) if (values?.length) { if (values.length > 50) throw new TypeError('too many filter values'); clauses.push(`${field} in (${values.map(value => { const trimmed = value.trim(); if (!trimmed) throw new TypeError('filter values cannot be blank'); return quoteJql(trimmed); }).join(', ')})`); }
  if (filter.statusCategories?.length) { const ids = { new: '2', indeterminate: '4', done: '3' } as Record<string, string>; clauses.push(`statusCategory in (${filter.statusCategories.map(category => { if (!ids[category]) throw new TypeError('invalid status category'); return ids[category]; }).join(', ')})`); }
  if (filter.assigneeAccountIds?.length) { if (filter.assigneeAccountIds.length > 50 || filter.assigneeAccountIds.some(id => !accountId.test(id))) throw new TypeError('invalid assignee ID'); const list = `assignee in (${filter.assigneeAccountIds.map(quoteJql).join(', ')})`; clauses.push(filter.includesUnassigned ? `(${list} OR assignee is EMPTY)` : list); }
  else if (filter.includesUnassigned) clauses.push('assignee is EMPTY');
  if (filter.summaryPhrase?.trim()) clauses.push('summary ~ ' + quoteJql(quoteJql(filter.summaryPhrase.trim())));
  if (!clauses.length) throw new TypeError('filter needs at least one clause');
  const order = filter.order || 'createdDescending'; const sort = { createdDescending: 'ORDER BY created DESC', createdAscending: 'ORDER BY created ASC', updatedDescending: 'ORDER BY updated DESC' } as Record<string, string>;
  if (!sort[order]) throw new TypeError('invalid filter order'); return clauses.join(' AND ') + ' ' + sort[order];
}

/** Create an issue behind a durable one-send fence. */
export class CreateIssueOperation implements Mutation<CreateIssueInput, CreateIssueOutput> {
  /** Bind a Jira client. */ constructor(private readonly client: JiraClient) {}
  /** Return the manifest definition. */ definition() { return CreateIssueDefinition; }
  /** Record the stable Dex Call ID without sending it as an idempotency header. */ idempotencyKey(callId: string): string { return callId; }
  /** Validate, fence, send, and reconcile a create. */
  async invoke(call: Call, input: CreateIssueInput): Promise<MutationAttempt<CreateIssueOutput>> {
    const requested: CreateIssueOutput = { ...(input.projectKey?.trim() ? { projectKey: input.projectKey.trim() } : {}), ...(input.projectId?.trim() ? { projectId: input.projectId.trim() } : {}), summary: input.summary?.trim() ?? '' };
    let fields: Record<string, unknown>;
    try { fields = buildIssueFields(input); }
    catch (error) { return newMutationBranch('defect', requested, {}, jiraFailure('createIssue', 'VALIDATION', message(error))); }
    const fence = newFence(call, CreateIssueDefinition.stepDefaults.executeMethodTimeoutMs ?? 30_000);
    const decision = fence.decide();
    if (decision.kind === 'failClosed') return newMutationUncertain(requested, jiraFailure('createIssue', 'PROTOCOL', 'Jira dispatch checkpoint cannot be trusted'), {});
    let session: JiraSession;
    try { session = await this.client.startSession(call, 'createIssue'); }
    catch (error) {
      if (decision.kind === 'sendAllowed') {
        try { await fence.recordNotSent(); }
        catch { return newMutationRetry(jiraFailure('createIssue', 'AVAILABILITY', 'Jira setup failed and the not-sent checkpoint could not be recorded')); }
        return mutationSessionFailure(error, requested, 'createIssue');
      }
      if (error instanceof JiraSessionFailure && error.retry) return newMutationRetry(error.failure, error.retryAfterMs);
      return newMutationUncertain(requested, error instanceof JiraSessionFailure ? error.failure : jiraFailure('createIssue', 'AVAILABILITY', 'Jira setup failed'), {});
    }
    const marker = { key: propertyKey, value: createHash('sha256').update(call.id).digest('hex') };
    const receipt = this.client.receipt(session);
    if (decision.kind === 'readBackRequired') return this.reconcile(session, requested, marker.value, decision.marker?.callId === call.id ? decision.marker.providerId ?? '' : '', jiraFailure('createIssue', 'AVAILABILITY', 'Jira issue dispatch may have succeeded'), receipt);
    let exchange: JiraExchange;
    try { exchange = await fence.dispatch(signal => this.client.exchangeFencedWrite(session, 'POST', '/issue', { fields, properties: [marker] }, signal)); }
    catch { return newMutationRetry(jiraFailure('createIssue', 'AVAILABILITY', 'Jira dispatch checkpoint or request budget is unavailable')); }
    const classified = classifyWrite('createIssue', 'issue', exchange);
    const response = 'response' in exchange ? exchange.response : undefined;
    const writeReceipt = this.client.receipt(session, response);
    if (classified.kind === 'retry') {
      try { await fence.recordCanResend(); }
      catch { return newMutationRetry(jiraFailure('createIssue', 'AVAILABILITY', 'Jira retry checkpoint could not be recorded'), classified.retryAfterMs); }
      return newMutationRetry(classified.failure, classified.retryAfterMs);
    }
    if (classified.kind === 'defect') return newMutationBranch('defect', requested, writeReceipt, classified.failure);
    if (classified.kind === 'uncertain') return this.reconcile(session, requested, marker.value, '', classified.failure, writeReceipt);
    if (classified.kind === 'rejected' || classified.kind === 'notFound') {
      if (response?.status === 401 && await this.client.refreshRejectedCredential(session)) {
        try { await fence.recordCanResend(); } catch { return newMutationRetry(jiraFailure('createIssue', 'AVAILABILITY', 'Jira credential retry checkpoint could not be recorded')); }
        return newMutationRetry(jiraFailure('createIssue', 'AUTHENTICATION', 'Jira access token was refreshed for retry'));
      }
      if (response?.status === 409) return this.reconcile(session, requested, marker.value, '', classified.failure, writeReceipt);
      return newMutationBranch('providerRejected', { ...requested, ...(classified.fields.length ? { rejectedFieldIds: classified.fields } : {}) }, writeReceipt, classified.failure);
    }
    try {
      const created = decodeObject(response!.body); const id = requireString(created.id);
      if (numericId.test(id)) {
        try { await fence.recordProviderId(id); } catch { /* An accepted write remains accepted when its read-back checkpoint fails. */ }
        const key = requireString(created.key);
        if (issueKey.test(key)) return newMutationBranch('created', { ...requested, issueId: id, issueKey: key }, { ...writeReceipt, providerObjectId: key });
        return this.reconcile(session, requested, marker.value, id, jiraFailure('createIssue', 'PROTOCOL', 'Jira accepted the issue but returned an unusable issue key'), writeReceipt);
      }
    } catch { /* Read-back resolves an accepted response with an unusable reference. */ }
    return this.reconcile(session, requested, marker.value, '', jiraFailure('createIssue', 'PROTOCOL', 'Jira accepted the issue but returned an unusable issue reference'), writeReceipt);
  }
  private async reconcile(session: JiraSession, requested: CreateIssueOutput, marker: string, providerId: string, failure: Failure, receipt: ReceiptDraft): Promise<MutationAttempt<CreateIssueOutput>> {
    let exchange: JiraExchange;
    if (providerId) exchange = await this.client.exchange(session, 'GET', `/issue/${pathEscape(providerId)}`, { fields: 'summary', properties: propertyKey });
    else {
      const project = requested.projectId || quoteJql(requested.projectKey ?? '');
      const jql = `project = ${project} AND summary ~ ${quoteJql(quoteJql(requested.summary))} ORDER BY created DESC`;
      exchange = await this.client.exchange(session, 'POST', '/search/jql', undefined, { jql, fields: ['summary'], properties: [propertyKey], maxResults: 50 });
    }
    const state = readbackState(exchange);
    if (state === 'retry') return newMutationRetry(jiraFailure('createIssue', 'AVAILABILITY', 'Jira issue read-back is temporarily unavailable'));
    if (state === 'missing') return newMutationUncertain(requested, failure, receipt);
    try {
      const body = decodeObject(('response' in exchange ? exchange.response : (() => { throw new TypeError('missing response'); })()).body);
      const candidates = providerId ? [body] : Array.isArray(body.issues) ? body.issues : [];
      for (const raw of candidates) {
        const item = requireObject(raw), id = requireString(item.id), key = requireString(item.key), fields = requireObject(item.fields ?? {});
        if (!numericId.test(id) || !issueKey.test(key) || (providerId && id !== providerId) || fields.summary !== requested.summary) continue;
        const properties = item.properties == null ? {} : requireObject(item.properties);
        let matches = matchesMarker(properties[propertyKey], marker);
        if (!(propertyKey in properties)) {
          const property = await this.client.exchange(session, 'GET', `/issue/${pathEscape(id)}/properties/${propertyKey}`);
          const propertyState = readbackState(property);
          if (propertyState === 'retry') return newMutationRetry(jiraFailure('createIssue', 'AVAILABILITY', 'Jira issue read-back is temporarily unavailable'));
          matches = propertyState === 'found' && matchesMarker(decodeObject(('response' in property ? property.response : (() => { throw new TypeError('missing response'); })()).body), marker);
        }
        if (matches) return newMutationBranch('created', { ...requested, issueId: id, issueKey: key }, { ...receipt, providerObjectId: key });
      }
    } catch { /* An invalid read-back leaves the non-idempotent write uncertain. */ }
    return newMutationUncertain(requested, failure, receipt);
  }
}

/** Add one issue comment behind a durable one-send fence. */
export class AddCommentOperation implements Mutation<AddCommentInput, AddCommentOutput> {
  /** Bind a Jira client. */ constructor(private readonly client: JiraClient) {}
  /** Return the manifest definition. */ definition() { return AddCommentDefinition; }
  /** Record the stable Dex Call ID. */ idempotencyKey(callId: string): string { return callId; }
  /** Validate, fence, send, and reconcile a comment. */
  async invoke(call: Call, input: AddCommentInput): Promise<MutationAttempt<AddCommentOutput>> {
    const requested: AddCommentOutput = { issueIdOrKey: input.issueIdOrKey, createdAt: zeroTime };
    let issue: string;
    try { issue = validateIssueIdOrKey(input.issueIdOrKey); validatePlainText(input.body, 'body', true); requested.issueIdOrKey = issue; }
    catch (error) { return newMutationBranch('defect', requested, {}, jiraFailure('addComment', 'VALIDATION', message(error))); }
    const fence = newFence(call, AddCommentDefinition.stepDefaults.executeMethodTimeoutMs ?? 30_000);
    const decision = fence.decide();
    if (decision.kind === 'failClosed') return newMutationUncertain(requested, jiraFailure('addComment', 'PROTOCOL', 'Jira dispatch checkpoint cannot be trusted'), {});
    let session: JiraSession;
    try { session = await this.client.startSession(call, 'addComment'); }
    catch (error) {
      if (decision.kind === 'sendAllowed') {
        try { await fence.recordNotSent(); } catch { return newMutationRetry(jiraFailure('addComment', 'AVAILABILITY', 'Jira setup failed and the not-sent checkpoint could not be recorded')); }
        return mutationSessionFailure(error, requested, 'addComment');
      }
      if (error instanceof JiraSessionFailure && error.retry) return newMutationRetry(error.failure, error.retryAfterMs);
      return newMutationUncertain(requested, error instanceof JiraSessionFailure ? error.failure : jiraFailure('addComment', 'AVAILABILITY', 'Jira setup failed'), {});
    }
    const marker = createHash('sha256').update(call.id).digest('hex');
    const receipt = this.client.receipt(session);
    if (decision.kind === 'readBackRequired') return this.reconcile(session, requested, marker, decision.marker?.callId === call.id ? decision.marker.providerId ?? '' : '', jiraFailure('addComment', 'AVAILABILITY', 'Jira comment dispatch may have succeeded'), receipt);
    let exchange: JiraExchange;
    try { exchange = await fence.dispatch(signal => this.client.exchangeFencedWrite(session, 'POST', `/issue/${pathEscape(issue)}/comment`, { body: plainTextDocument(input.body), properties: [{ key: propertyKey, value: marker }] }, signal)); }
    catch { return newMutationRetry(jiraFailure('addComment', 'AVAILABILITY', 'Jira dispatch checkpoint or request budget is unavailable')); }
    const classified = classifyWrite('addComment', 'comment', exchange), response = 'response' in exchange ? exchange.response : undefined;
    const writeReceipt = this.client.receipt(session, response);
    if (classified.kind === 'retry') {
      try { await fence.recordCanResend(); } catch { return newMutationRetry(jiraFailure('addComment', 'AVAILABILITY', 'Jira retry checkpoint could not be recorded'), classified.retryAfterMs); }
      return newMutationRetry(classified.failure, classified.retryAfterMs);
    }
    if (classified.kind === 'defect') return newMutationBranch('defect', requested, writeReceipt, classified.failure);
    if (classified.kind === 'uncertain') return this.reconcile(session, requested, marker, '', classified.failure, writeReceipt);
    if (classified.kind === 'notFound') return newMutationBranch('notFound', requested, writeReceipt, classified.failure);
    if (classified.kind === 'rejected') {
      if (response?.status === 401 && await this.client.refreshRejectedCredential(session)) {
        try { await fence.recordCanResend(); } catch { return newMutationRetry(jiraFailure('addComment', 'AVAILABILITY', 'Jira credential retry checkpoint could not be recorded')); }
        return newMutationRetry(jiraFailure('addComment', 'AUTHENTICATION', 'Jira access token was refreshed for retry'));
      }
      if (response?.status === 409) return this.reconcile(session, requested, marker, '', classified.failure, writeReceipt);
      return newMutationBranch('providerRejected', { ...requested, ...(classified.fields.length ? { rejectedFieldIds: classified.fields } : {}) }, writeReceipt, classified.failure);
    }
    try { const item = decodeObject(response!.body), id = requireString(item.id); if (numericId.test(id)) { try { await fence.recordProviderId(id); } catch { /* The provider accepted this comment; read-back remains safe. */ } return this.added(requested, item, writeReceipt); } }
    catch { /* Read-back resolves an accepted response with an unusable comment reference. */ }
    return this.reconcile(session, requested, marker, '', jiraFailure('addComment', 'PROTOCOL', 'Jira accepted the comment but returned an unusable comment reference'), writeReceipt);
  }
  private added(requested: AddCommentOutput, item: Record<string, unknown>, receipt: ReceiptDraft): MutationAttempt<AddCommentOutput> {
    const id = requireString(item.id), author = item.author == null ? {} : requireObject(item.author);
    let createdAt = zeroTime; try { createdAt = parseJiraTime(item.created); } catch { /* A stored comment remains successful when Jira returns an invalid timestamp. */ }
    return newMutationBranch('added', { ...requested, commentId: id, createdAt, ...(author.accountId ? { authorAccountId: requireString(author.accountId) } : {}) }, { ...receipt, providerObjectId: id });
  }
  private async reconcile(session: JiraSession, requested: AddCommentOutput, marker: string, providerId: string, failure: Failure, receipt: ReceiptDraft): Promise<MutationAttempt<AddCommentOutput>> {
    const issuePath = `/issue/${pathEscape(requested.issueIdOrKey)}/comment`;
    const exchange = providerId ? await this.client.exchange(session, 'GET', `${issuePath}/${pathEscape(providerId)}`, { expand: 'properties' }) : await this.client.exchange(session, 'GET', issuePath, { expand: 'properties', maxResults: '50', orderBy: '-created' });
    const state = readbackState(exchange);
    if (state === 'retry') return newMutationRetry(jiraFailure('addComment', 'AVAILABILITY', 'Jira comment read-back is temporarily unavailable'));
    if (state === 'missing') return newMutationUncertain(requested, failure, receipt);
    try {
      const body = decodeObject(('response' in exchange ? exchange.response : (() => { throw new TypeError('missing response'); })()).body), candidates = providerId ? [body] : Array.isArray(body.comments) ? body.comments : [];
      for (const raw of candidates) {
        const item = requireObject(raw), id = requireString(item.id); if (!numericId.test(id) || (providerId && id !== providerId)) continue;
        const properties = item.properties;
        let matches = Array.isArray(properties) && properties.some(value => { const property = requireObject(value); return property.key === propertyKey && typeof property.value === 'string' && property.value.toLowerCase() === marker; });
        if (!Array.isArray(properties) || !properties.length) {
          const property = await this.client.exchange(session, 'GET', `/comment/${pathEscape(id)}/properties/${propertyKey}`);
          const propertyState = readbackState(property);
          if (propertyState === 'retry') return newMutationRetry(jiraFailure('addComment', 'AVAILABILITY', 'Jira comment read-back is temporarily unavailable'));
          matches = propertyState === 'found' && matchesMarker(decodeObject(('response' in property ? property.response : (() => { throw new TypeError('missing response'); })()).body), marker);
        }
        if (matches) return this.added(requested, item, receipt);
      }
    } catch { /* An invalid read-back leaves the non-idempotent write uncertain. */ }
    return newMutationUncertain(requested, failure, receipt);
  }
}

function buildIssueFields(input: CreateIssueInput): Record<string, unknown> {
  const key = (input.projectKey ?? '').trim(), id = (input.projectId ?? '').trim();
  if (!!key === !!id || (key && !projectKey.test(key)) || (id && !numericId.test(id))) throw new TypeError('set exactly one valid projectKey or projectId');
  const typeName = (input.issueTypeName ?? '').trim(), typeId = (input.issueTypeId ?? '').trim();
  if (!!typeName === !!typeId || (typeId && !numericId.test(typeId))) throw new TypeError('set exactly one valid issueTypeName or issueTypeId');
  if (typeName) validateSingleLine(typeName, 'issueTypeName', 255);
  const summary = validateSingleLine(input.summary, 'summary', 255); validatePlainText(input.description ?? '', 'description', false);
  const labels = input.labels ?? []; if (labels.length > 50 || labels.some(label => !isValidUnicode(label) || !label || [...label].length > 255 || /[\x00-\x20\x7f\x85\xa0\u2028\u2029]/u.test(label))) throw new TypeError('labels are invalid');
  const assignee = (input.assigneeAccountId ?? '').trim(); if (assignee && !accountId.test(assignee)) throw new TypeError('assigneeAccountId must be an Atlassian account ID');
  return { project: key ? { key } : { id }, issuetype: typeName ? { name: typeName } : { id: typeId }, summary, ...(input.description?.trim() ? { description: plainTextDocument(input.description) } : {}), ...(labels.length ? { labels } : {}), ...(assignee ? { assignee: { accountId: assignee } } : {}) };
}
function validateSingleLine(value: string, field: string, limit: number): string { const trimmed = value.trim(); if (!trimmed || !isValidUnicode(trimmed) || [...trimmed].length > limit || /[\x00-\x1f\x7f]/.test(trimmed)) throw new TypeError(`${field} is invalid`); return trimmed; }
function validatePlainText(value: string, field: string, required: boolean): void { if (!isValidUnicode(value) || (required && !value.trim()) || [...value].length > 32767 || /[\x00-\x08\x0b\x0c\x0e-\x1f]/.test(value)) throw new TypeError(`${field} is invalid`); }
function plainTextDocument(value: string): Record<string, unknown> { const normalized = value.replace(/\r\n?/g, '\n').replace(/^\n+|\n+$/g, ''); return { type: 'doc', version: 1, ...(normalized ? { content: normalized.split(/\n[ \t]*\n+/).map(paragraph => ({ type: 'paragraph', content: paragraph.split('\n').flatMap((line, index) => [...(index ? [{ type: 'hardBreak' }] : []), ...(line ? [{ type: 'text', text: line }] : [])]) })).filter(paragraph => paragraph.content.length) } : {}) }; }
function matchesMarker(value: unknown, expected: string): boolean { if (typeof value === 'string') return value.toLowerCase() === expected; if (value && typeof value === 'object' && !Array.isArray(value)) return typeof (value as Record<string, unknown>).value === 'string' && ((value as Record<string, unknown>).value as string).toLowerCase() === expected; return false; }
function readbackState(exchange: JiraExchange): 'found' | 'missing' | 'retry' { if ('error' in exchange) return 'retry'; const status = exchange.response.status; if (status === 408 || status === 429 || status >= 500) return 'retry'; return status === 200 && !exchange.isTooLarge && !exchange.reflectsSecret ? 'found' : 'missing'; }
type WriteClassification = { kind: 'accepted' } | { kind: 'retry' | 'uncertain' | 'rejected' | 'notFound' | 'defect'; failure: Failure; fields: string[]; retryAfterMs: number };
function classifyWrite(operation: string, subject: string, exchange: JiraExchange): WriteClassification {
  const result = (kind: Exclude<WriteClassification['kind'], 'accepted'>, failure: Failure, fields: string[] = [], retryAfterMs = 0): WriteClassification => ({ kind, failure, fields, retryAfterMs });
  if ('error' in exchange) return exchange.isProvablyNotSent ? result('retry', jiraFailure(operation, 'TRANSPORT', `Jira could not be reached, so no ${subject} was written`)) : result('uncertain', jiraFailure(operation, 'TRANSPORT', `Jira ${subject} outcome is unknown`));
  const { response } = exchange, status = response.status;
  if (status >= 200 && status < 300) {
    if (exchange.isTooLarge) return result('uncertain', jiraFailure(operation, 'RESPONSE_TOO_LARGE', `Jira accepted the ${subject} but its response exceeds the configured size limit`));
    if (exchange.reflectsSecret) return result('uncertain', jiraFailure(operation, 'PROTOCOL', `Jira accepted the ${subject} but its response contains credential material`));
    return { kind: 'accepted' };
  }
  if (status === 429) return result('retry', jiraFailure(operation, 'RATE_LIMIT', `Jira rate limited the ${subject} before writing it`), [], retryAfter(response));
  if (status === 503) return result('retry', jiraFailure(operation, 'AVAILABILITY', `Jira ${subject} is temporarily unavailable`), [], retryAfter(response));
  if (status === 404) return result('notFound', rejection(operation, subject, status, []));
  if (status >= 400 && status < 500 && status !== 408 && status !== 429) { const fields = rejectedFields(response.body); return result('rejected', rejection(operation, subject, status, fields), fields); }
  return result('uncertain', jiraFailure(operation, 'AVAILABILITY', `Jira ${subject} outcome is unknown after HTTP ${status}`));
}

/** Move an issue through one available workflow transition. */
export class TransitionIssueOperation implements Mutation<TransitionIssueInput, TransitionIssueOutput> {
  /** Bind a Jira client. */ constructor(private readonly client: JiraClient) {}
  /** Return the manifest definition. */ definition() { return TransitionIssueDefinition; }
  /** Record the stable Call ID; Jira makes the state transition itself repeat-safe. */ idempotencyKey(callId: string): string { return callId; }
  /** Read available transitions, send one match, and verify ambiguity. */
  async invoke(call: Call, input: TransitionIssueInput): Promise<MutationAttempt<TransitionIssueOutput>> {
    let issue: string, transitionId: string, transitionName: string, destinationName: string, resolutionName: string;
    try {
      issue = validateIssueIdOrKey(input.issueIdOrKey); transitionId = (input.transitionId ?? '').trim();
      if (transitionId && !numericId.test(transitionId)) throw new TypeError('transitionId must be a numeric transition ID');
      transitionName = input.transitionName?.trim() ?? ''; destinationName = input.destinationStatusName?.trim() ?? ''; resolutionName = input.resolutionName?.trim() ?? '';
      for (const [name, value] of [['transitionName', transitionName], ['destinationStatusName', destinationName], ['resolutionName', resolutionName]]) if (value) validateSingleLine(value, name!, 255);
      if (!transitionId && !transitionName && !destinationName) throw new TypeError('set transitionId, transitionName, or destinationStatusName');
    } catch (error) { return newMutationBranch('defect', zeroTransition, {}, jiraFailure('transitionIssue', 'VALIDATION', message(error))); }
    let session: JiraSession;
    try { session = await this.client.startSession(call, 'transitionIssue'); }
    catch (error) { return mutationSessionFailure(error, zeroTransition, 'transitionIssue'); }
    const first = await this.readTransitions(session, issue);
    if (first.kind !== 'ok') return this.readFailure(first);
    const snapshot = first.snapshot, receipt = first.receipt;
    const output: TransitionIssueOutput = { issueId: snapshot.issueId, issueKey: snapshot.issueKey, previousStatus: snapshot.status, status: snapshot.status };
    if (destinationName && snapshot.status.name.toLowerCase() === destinationName.toLowerCase()) return newMutationBranch('transitioned', { ...output, isAlreadyInDestinationStatus: true }, receipt);
    const matches = snapshot.transitions.filter(transition => (!transitionId || transition.id === transitionId) && (!transitionName || transition.name.trim().toLowerCase() === transitionName.toLowerCase()) && (!destinationName || transition.destinationStatus.name.toLowerCase() === destinationName.toLowerCase()));
    if (matches.length !== 1) return newMutationBranch('transitionUnavailable', { ...output, ...(snapshot.transitions.length ? { availableTransitions: snapshot.transitions } : {}) }, receipt, jiraFailure('transitionIssue', matches.length ? 'VALIDATION' : 'CONFLICT', matches.length ? `${matches.length} transitions available from status ${snapshot.status.id} match the request; set transitionId` : `no transition available from status ${snapshot.status.id} matches the request`));
    const chosen = matches[0]!; output.transition = chosen;
    if (chosen.destinationStatus.id === snapshot.status.id) return newMutationBranch('transitioned', { ...output, isAlreadyInDestinationStatus: true }, receipt);
    const payload = { transition: { id: chosen.id }, ...(resolutionName ? { fields: { resolution: { name: resolutionName } } } : {}) };
    const write = await this.client.exchange(session, 'POST', `/issue/${pathEscape(issue)}/transitions`, undefined, payload);
    const writeResponse = 'response' in write ? write.response : undefined;
    const writeReceipt = this.client.receipt(session, writeResponse, snapshot.issueKey);
    if (writeResponse && writeResponse.status >= 200 && writeResponse.status < 300) return newMutationBranch('transitioned', { ...output, status: chosen.destinationStatus }, writeReceipt);
    if (writeResponse?.status === 404) return newMutationBranch('notFound', output, writeReceipt, jiraFailure('transitionIssue', 'NOT_FOUND', 'Jira issue was not found or is not visible to the connection'));
    if (writeResponse?.status === 429) return newMutationRetry(jiraFailure('transitionIssue', 'RATE_LIMIT', 'Jira rate limited the transition before performing it'), retryAfter(writeResponse));
    if ('error' in write && write.isProvablyNotSent) return newMutationRetry(jiraFailure('transitionIssue', 'TRANSPORT', 'Jira could not be reached, so the issue was not transitioned'));
    const reread = await this.readTransitions(session, issue);
    if (reread.kind !== 'ok') return newMutationRetry(jiraFailure('transitionIssue', 'AVAILABILITY', 'Jira transition outcome could not be read back'));
    if (reread.snapshot.status.id === chosen.destinationStatus.id) return newMutationBranch('transitioned', { ...output, status: reread.snapshot.status }, writeReceipt);
    if (writeResponse && writeResponse.status >= 400 && writeResponse.status < 500 && writeResponse.status !== 408 && writeResponse.status !== 429 && writeResponse.status !== 409) {
      const fields = rejectedFields(writeResponse.body);
      return newMutationBranch('providerRejected', { ...output, status: reread.snapshot.status, ...(fields.length ? { rejectedFieldIds: fields } : {}) }, writeReceipt, rejection('transitionIssue', 'transition', writeResponse.status, fields));
    }
    if (writeResponse && writeResponse.status < 400) return newMutationBranch('invalidResponse', output, writeReceipt, jiraFailure('transitionIssue', 'PROTOCOL', `Jira returned unexpected HTTP ${writeResponse.status} for the transition`));
    if (writeResponse?.status === 409) return newMutationRetry(jiraFailure('transitionIssue', 'CONFLICT', 'Jira reported a conflicting update, so the transition is retried'), retryAfter(writeResponse));
    return newMutationRetry(jiraFailure('transitionIssue', 'AVAILABILITY', 'Jira did not confirm the transition and the issue has not moved yet'), writeResponse ? retryAfter(writeResponse) : 0);
  }
  private async readTransitions(session: JiraSession, issue: string): Promise<{ kind: 'ok'; snapshot: { issueId: string; issueKey: string; status: IssueStatus; transitions: TransitionReference[] }; receipt: ReceiptDraft } | { kind: 'retry'; failure: Failure; retryAfterMs: number } | { kind: 'branch'; branch: string; failure: Failure; receipt: ReceiptDraft }> {
    const exchange = await this.client.exchange(session, 'GET', `/issue/${pathEscape(issue)}`, { expand: 'transitions', fields: 'status' });
    const classified = classifyRead(this.client, session, 'transitionIssue', 'issue', exchange, issue);
    if (classified.kind !== 'ok') return classified;
    try {
      const value = decodeObject(classified.response.body), id = requireString(value.id), key = requireString(value.key), fields = requireObject(value.fields);
      if (!numericId.test(id) || !issueKey.test(key) || fields.status == null) throw new TypeError('invalid issue');
      const status = decodeStatus(fields.status), transitions: TransitionReference[] = [];
      if (value.transitions != null && !Array.isArray(value.transitions)) throw new TypeError('invalid transition list');
      for (const candidate of value.transitions ?? []) {
        const item = requireObject(candidate); if (!item.id || item.to == null || transitions.length === 50) continue;
        transitions.push({ id: requireString(item.id), name: optionalString(item.name), destinationStatus: decodeStatus(item.to) });
      }
      return { kind: 'ok', snapshot: { issueId: id, issueKey: key, status, transitions }, receipt: classified.receipt };
    } catch { return { kind: 'branch', branch: 'invalidResponse', failure: jiraFailure('transitionIssue', 'PROTOCOL', 'Jira returned an invalid issue or transition list'), receipt: classified.receipt }; }
  }
  private readFailure(failure: { kind: 'retry'; failure: Failure; retryAfterMs: number } | { kind: 'branch'; branch: string; failure: Failure; receipt: ReceiptDraft }): MutationAttempt<TransitionIssueOutput> {
    if (failure.kind === 'retry') return newMutationRetry(failure.failure, failure.retryAfterMs);
    return newMutationBranch(failure.branch === 'rejected' ? 'providerRejected' : failure.branch, zeroTransition, failure.receipt, failure.failure);
  }
}

function isValidUnicode(value: string): boolean { for (let index = 0; index < value.length; index++) { const code = value.charCodeAt(index); if (code >= 0xd800 && code <= 0xdbff) { const next = value.charCodeAt(++index); if (!(next >= 0xdc00 && next <= 0xdfff)) return false; } else if (code >= 0xdc00 && code <= 0xdfff) return false; } return true; }
