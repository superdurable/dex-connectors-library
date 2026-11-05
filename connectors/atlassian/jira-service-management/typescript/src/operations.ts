// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { isIP } from 'node:net';
import { newMutationBranch, newMutationRetry, newMutationUncertain, newQueryBranch, newQueryRetry, type Call, type Failure, type Mutation, type MutationAttempt, type Query, type QueryAttempt, type ReceiptDraft } from '@superdurable/connectors-sdk';
import { AddCommentDefinition, CreateTicketDefinition, FindCustomerByEmailDefinition, GetTicketDefinition, SearchTicketsDefinition, TransitionTicketDefinition, UpdateTicketDefinition } from './generated/definitions.js';
import type { AddCommentInput, AddCommentOutput, CreateTicketInput, CreateTicketOutput, Customer, CustomerMatches, FindCustomerByEmailInput, GetTicketInput, SearchTicketsInput, SearchTicketsOutput, Ticket, TicketDetails, TicketStatus, TransitionTicketInput, TransitionTicketOutput, UpdateTicketInput, UpdateTicketOutput } from './generated/models.js';
import { isRecord, pathEscape, type Classification, type JiraResult, type JiraServiceManagementClient } from './client.js';

const numericID = /^[1-9][0-9]{0,17}$/;
const issueKey = /^[A-Za-z][A-Za-z0-9_]*-[1-9][0-9]{0,17}$/;
const accountID = /^[A-Za-z0-9:_-]{1,128}$/;
const fieldID = /^[A-Za-z][A-Za-z0-9_.-]{0,63}$/;
const ticketFields = ['summary', 'status', 'project', 'assignee', 'reporter', 'priority', 'labels', 'created', 'updated'];
const zeroTime = '0001-01-01T00:00:00Z';
const zeroStatus: TicketStatus = { id: '', name: '' };
const zeroTicket: Ticket = { id: '', key: '', summary: '', status: zeroStatus, createdAt: zeroTime, updatedAt: zeroTime };
const zeroDetails: TicketDetails = { ticket: zeroTicket, comments: [], slas: [] };
const zeroTransition: TransitionTicketOutput = { issueId: '', issueKey: '', previousStatus: zeroStatus, status: zeroStatus };

function failure(operation: string, kind: Failure['kind'], message: string): Failure { return { kind, provider: 'jira-service-management', operation, message }; }
function queryDefect<T>(operation: string, output: T, error: unknown): QueryAttempt<T> { return newQueryBranch('defect', output, {}, failure(operation, 'VALIDATION', error instanceof Error ? error.message : 'invalid input')); }
function mutationDefect<T>(operation: string, output: T, error: unknown): MutationAttempt<T> { return newMutationBranch('defect', output, {}, failure(operation, 'VALIDATION', error instanceof Error ? error.message : 'invalid input')); }
function queryClassified<T>(classification: Classification, output: T, notFound = 'notFound'): QueryAttempt<T> {
  if (classification.kind === 'retry') return newQueryRetry(classification.failure!, classification.retryAfterMs);
  return newQueryBranch(classification.kind === 'notFound' ? notFound : classification.kind === 'rejected' ? 'providerRejected' : classification.kind, output, classification.receipt, classification.failure);
}
function mutationClassified<T>(classification: Classification, output: T, notFound = 'notFound'): MutationAttempt<T> {
  if (classification.kind === 'retry') return newMutationRetry(classification.failure!, classification.retryAfterMs);
  return newMutationBranch(classification.kind === 'notFound' ? notFound : classification.kind === 'rejected' ? 'providerRejected' : classification.kind, output, classification.receipt, classification.failure);
}
function parsedObject(result: JiraResult): Record<string, unknown> {
  const parsed: unknown = JSON.parse(Buffer.from(result.response?.body ?? []).toString('utf8'));
  if (!isRecord(parsed)) throw new TypeError('provider object is invalid');
  return parsed;
}
function requiredString(value: unknown): string { if (typeof value !== 'string') throw new TypeError('provider string is invalid'); return value; }
function optionalString(value: unknown): string { return value === undefined || value === null ? '' : requiredString(value); }
function requiredBoolean(value: unknown): boolean { if (typeof value !== 'boolean') throw new TypeError('provider boolean is invalid'); return value; }
function requiredArray(value: unknown): unknown[] { if (!Array.isArray(value)) throw new TypeError('provider list is invalid'); return value; }
function serviceDeskPage(result: JiraResult): { values: unknown[]; isLastPage: boolean } {
  const page = parsedObject(result);
  for (const field of ['start', 'size'] as const) if (page[field] !== undefined && (!Number.isSafeInteger(page[field]) || typeof page[field] !== 'number')) throw new TypeError('page count invalid');
  if (page.isLastPage !== undefined && typeof page.isLastPage !== 'boolean') throw new TypeError('page flag invalid');
  return { values: requiredArray(page.values), isLastPage: page.isLastPage === true };
}
function trimID(value: string, name: string, pattern = numericID): string { const trimmed = value.trim(); if (!pattern.test(trimmed)) throw new TypeError(`${name} is invalid`); return trimmed; }
function issuePath(value: string): string { return `/issue/${pathEscape(value)}`; }
function requestPath(value: string): string { return `/request/${pathEscape(value)}`; }
function invalid<T>(operation: string, output: T, receipt: ReceiptDraft): QueryAttempt<T> { return newQueryBranch('invalidResponse', output, receipt, failure(operation, 'PROTOCOL', 'Jira returned an invalid response')); }
function invalidMutation<T>(operation: string, output: T, receipt: ReceiptDraft): MutationAttempt<T> { return newMutationBranch('invalidResponse', output, receipt, failure(operation, 'PROTOCOL', 'Jira returned an invalid response')); }

/** Read exact email matches from one service desk customer page. */
export class FindCustomerByEmailOperation implements Query<FindCustomerByEmailInput, CustomerMatches> {
  /** Bind the shared client. */ constructor(private readonly client: JiraServiceManagementClient) {}
  /** Return the manifest definition. */ definition() { return FindCustomerByEmailDefinition; }
  /** Perform one bounded lookup. */
  async invoke(call: Call, input: FindCustomerByEmailInput): Promise<QueryAttempt<CustomerMatches>> {
    let desk: string;
    let email: string;
    try {
      desk = trimID(input.serviceDeskId, 'serviceDeskId');
      email = validateEmail(input.email);
    } catch (error) { return queryDefect('findCustomerByEmail', { email: '', customers: [] }, error); }
    const output: CustomerMatches = { email, customers: [] };
    const session = await this.client.start(call, 'findCustomerByEmail');
    if ('kind' in session) return queryClassified(session, output, 'providerRejected');
    const result = await this.client.exchange(call, session, { method: 'GET', api: 'desk', path: `/servicedesk/${pathEscape(desk)}/customer`, query: { query: email, start: '0', limit: '50' }, experimental: true });
    const classification = this.client.read(call, 'findCustomerByEmail', 'customer lookup', result);
    if (classification.kind !== 'success') return queryClassified(classification, output, 'providerRejected');
    try {
      const page = serviceDeskPage(result);
      const values = page.values;
      if (!page.isLastPage) output.hasMore = true;
      for (const value of values) {
        if (!isRecord(value)) throw new TypeError('customer invalid');
        const candidateEmail = optionalString(value.emailAddress);
        const id = optionalString(value.accountId);
        if (candidateEmail.trim().toLowerCase() !== email.toLowerCase() || !accountID.test(id)) continue;
        const customer: Customer = { accountId: id, isActive: value.active === null || value.active === undefined ? true : requiredBoolean(value.active) };
        if (value.displayName) customer.displayName = requiredString(value.displayName);
        output.customers!.push(customer);
      }
      if (!output.customers!.length) return newQueryBranch('notFound', output, classification.receipt, failure('findCustomerByEmail', 'NOT_FOUND', 'no customer of the service desk has this email address'));
      return newQueryBranch('found', output, { ...classification.receipt, providerObjectId: output.customers![0]!.accountId });
    } catch { return invalid('findCustomerByEmail', { email, customers: [] }, classification.receipt); }
  }
}

/** Search one enhanced Jira issue page. */
export class SearchTicketsOperation implements Query<SearchTicketsInput, SearchTicketsOutput> {
  /** Bind the shared client. */ constructor(private readonly client: JiraServiceManagementClient) {}
  /** Return the manifest definition. */ definition() { return SearchTicketsDefinition; }
  /** Build JQL, send, and decode one page. */
  async invoke(call: Call, input: SearchTicketsInput): Promise<QueryAttempt<SearchTicketsOutput>> {
    let body: { jql: string; fields: string[]; maxResults: number; nextPageToken?: string };
    try { body = searchBody(input); } catch (error) { return queryDefect('searchTickets', { jql: '', tickets: [] }, error); }
    const output: SearchTicketsOutput = { jql: body.jql, tickets: [] };
    const session = await this.client.start(call, 'searchTickets');
    if ('kind' in session) return queryClassified(session, output);
    const result = await this.client.exchange(call, session, { method: 'POST', api: 'platform', path: '/search/jql', payload: body });
    const classification = this.client.read(call, 'searchTickets', 'request search', result);
    if (classification.kind !== 'success') return queryClassified(classification, output, 'providerRejected');
    try {
      const page = parsedObject(result);
      const issues = page.issues === undefined || page.issues === null ? [] : requiredArray(page.issues);
      output.tickets = issues.map(issue => decodeTicket(issue, false));
      const token = optionalString(page.nextPageToken);
      if (Buffer.byteLength(token) > 4096) throw new TypeError('nextPageToken invalid');
      if (token) output.nextPageToken = token;
      return newQueryBranch('searched', output, classification.receipt);
    } catch { return invalid('searchTickets', { jql: body.jql, tickets: [] }, classification.receipt); }
  }
}

function validateEmail(value: string): string {
  const trimmed = value.trim();
  const at = trimmed.indexOf('@');
  const local = trimmed.slice(0, at);
  const domain = trimmed.slice(at + 1);
  const atom = (part: string): boolean => !!part && !part.startsWith('.') && !part.endsWith('.') && !part.includes('..') && [...part].every(character => {
    const code = character.codePointAt(0)!;
    return code >= 128 || code >= 33 && code <= 126 && !'()<>[]:;@\\,"'.includes(character);
  });
  const domainValid = domain.startsWith('[') && domain.endsWith(']') ? isIP(domain.slice(1, -1)) !== 0 : atom(domain);
  if (at < 1 || !atom(local) || !domainValid || Buffer.byteLength(trimmed) > 254) throw new TypeError('email must be one bare address such as jane@example.com');
  return trimmed;
}
function quoteJQL(value: string): string { return JSON.stringify(value); }
function searchBody(input: SearchTicketsInput): { jql: string; fields: string[]; maxResults: number; nextPageToken?: string } {
  const project = input.projectKey.trim();
  if (!/^[A-Z][A-Z0-9_]{0,254}$/.test(project)) throw new TypeError('projectKey invalid');
  const clauses = [`project = ${quoteJQL(project)}`];
  if (input.statusCategories?.length) {
    const ids: Record<string, string> = { new: '2', indeterminate: '4', done: '3' };
    const selected = input.statusCategories.map(category => { if (!ids[category]) throw new TypeError('statusCategories invalid'); return ids[category]; });
    clauses.push(`statusCategory in (${selected.join(', ')})`);
  }
  for (const [field, values] of [['status', input.statusNames], ['labels', input.labels], ['reporter', input.reporterAccountIds]] as const) {
    if (!values?.length) continue;
    if (values.length > 50) throw new TypeError(`${field} has too many values`);
    clauses.push(`${field} in (${values.map(value => { const trimmed = value.trim(); if (!trimmed || [...trimmed].length > 1024 || hasControl(trimmed) || (field === 'reporter' && !accountID.test(value))) throw new TypeError(`${field} invalid`); return quoteJQL(trimmed); }).join(', ')})`);
  }
  const phrase = input.summaryPhrase?.trim();
  if (phrase) clauses.push(`summary ~ ${quoteJQL(quoteJQL(phrase))}`);
  const since = input.updatedSinceDate?.trim();
  if (since) {
    const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(since);
    if (!match || new Date(`${since}T00:00:00Z`).toISOString().slice(0, 10) !== since) throw new TypeError('updatedSinceDate invalid');
    clauses.push(`updated >= ${quoteJQL(since)}`);
  }
  const order: Record<string, string> = { createdDescending: 'ORDER BY created DESC', updatedDescending: 'ORDER BY updated DESC', createdAscending: 'ORDER BY created ASC' };
  if (input.order && !order[input.order]) throw new TypeError('order invalid');
  const pageSize = input.pageSize || 50;
  if (pageSize < 1 || pageSize > 100) throw new TypeError('pageSize invalid');
  const token = input.nextPageToken?.trim() || '';
  if (Buffer.byteLength(token) > 4096 || (token && !/^[!-~]+$/.test(token))) throw new TypeError('nextPageToken invalid');
  return { jql: `${clauses.join(' AND ')} ${order[input.order || 'createdDescending']}`, fields: ticketFields, maxResults: pageSize, ...(token ? { nextPageToken: token } : {}) };
}

function decodeTicket(value: unknown, includeDescription: boolean): Ticket {
  if (!isRecord(value)) throw new TypeError('issue invalid');
  const id = requiredString(value.id), key = requiredString(value.key);
  if (!numericID.test(id) || !issueKey.test(key)) throw new TypeError('issue ID invalid');
  if (value.fields !== undefined && value.fields !== null && !isRecord(value.fields)) throw new TypeError('issue fields invalid');
  const fields = isRecord(value.fields) ? value.fields : {};
  for (const name of ['status', 'project', 'assignee', 'reporter', 'priority'] as const) if (fields[name] !== undefined && fields[name] !== null && !isRecord(fields[name])) throw new TypeError(`${name} invalid`);
  const status = isRecord(fields.status) ? fields.status : {};
  if (status.statusCategory !== undefined && status.statusCategory !== null && !isRecord(status.statusCategory)) throw new TypeError('status category invalid');
  const category = isRecord(status.statusCategory) ? status.statusCategory : {};
  const ticket: Ticket = { id, key, summary: optionalString(fields.summary), status: { id: optionalString(status.id), name: optionalString(status.name), ...(category.key ? { categoryKey: requiredString(category.key) } : {}) }, createdAt: jiraTime(optionalString(fields.created)), updatedAt: jiraTime(optionalString(fields.updated)) };
  if (isRecord(fields.project) && fields.project.key) ticket.projectKey = requiredString(fields.project.key);
  if (isRecord(fields.priority)) ticket.priority = { id: optionalString(fields.priority.id), name: optionalString(fields.priority.name) };
  for (const field of ['assignee', 'reporter'] as const) if (isRecord(fields[field]) && fields[field].accountId) ticket[field] = { accountId: requiredString(fields[field].accountId), ...(fields[field].displayName ? { displayName: requiredString(fields[field].displayName) } : {}) };
  if (fields.labels !== undefined && fields.labels !== null) ticket.labels = requiredArray(fields.labels).map(requiredString);
  if (includeDescription) { const extracted = extractDocumentText(fields.description); if (extracted.text) ticket.description = extracted.text; if (extracted.truncated) ticket.isDescriptionTruncated = true; }
  return ticket;
}
function jiraTime(value: string): string {
  if (!value) return zeroTime;
  const parts = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:[.,](\d+))?(Z|[+-]\d{2}:\d{2}|[+-]\d{4})$/.exec(value);
  if (!parts) throw new TypeError('Jira time invalid');
  const year = Number(parts[1]), month = Number(parts[2]), day = Number(parts[3]);
  const hour = Number(parts[4]), minute = Number(parts[5]), second = Number(parts[6]);
  if (!isCalendarDate(year, month, day) || hour > 23 || minute > 59 || second > 59) throw new TypeError('Jira time invalid');
  const zone = parts[8]!;
  const zoneHour = zone === 'Z' ? 0 : Number(zone.slice(1, 3));
  const zoneMinute = zone === 'Z' ? 0 : Number(zone.slice(-2));
  if (zoneMinute > 59 || zoneHour > 99) throw new TypeError('Jira time invalid');
  const utc = new Date(0);
  utc.setUTCFullYear(year, month - 1, day);
  utc.setUTCHours(hour, minute, second, 0);
  const offset = zone === 'Z' ? 0 : (zone[0] === '+' ? 1 : -1) * (zoneHour * 60 + zoneMinute);
  const adjusted = new Date(utc.getTime() - offset * 60_000);
  const nano = (parts[7] ?? '').slice(0, 9).padEnd(9, '0').replace(/0+$/, '');
  return adjusted.toISOString().slice(0, 19) + (nano ? `.${nano}` : '') + 'Z';
}
function isCalendarDate(year: number, month: number, day: number): boolean {
  if (month < 1 || month > 12 || day < 1) return false;
  const leap = year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0);
  return day <= [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31][month - 1]!;
}
function extractDocumentText(value: unknown): { text: string; truncated: boolean } {
  if (value === null || value === undefined) return { text: '', truncated: false };
  let text = '';
  let length = 0;
  let truncated = false;
  const append = (fragment: string): void => {
    for (const character of fragment) {
      if (length >= 32767) { truncated = true; return; }
      text += character;
      length++;
    }
  };
  if (typeof value === 'string') { append(value); return { text, truncated }; }
  if (!isRecord(value)) throw new TypeError('description invalid');
  const node = (item: unknown, depth: number): void => {
    if (depth > 48 || truncated) return;
    if (!isRecord(item) || item.type !== undefined && typeof item.type !== 'string' || item.text !== undefined && typeof item.text !== 'string' || item.content !== undefined && item.content !== null && !Array.isArray(item.content) || item.attrs !== undefined && item.attrs !== null && !isRecord(item.attrs)) throw new TypeError('ADF node invalid');
    const kind = optionalString(item.type);
    const children = item.content === null || item.content === undefined ? [] : item.content as unknown[];
    const attribute = (key: string): string => isRecord(item.attrs) && typeof item.attrs[key] === 'string' ? item.attrs[key] : '';
    if (kind === 'text') append(optionalString(item.text));
    else if (kind === 'hardBreak') append('\n');
    else if (['mention', 'emoji', 'status'].includes(kind)) append(attribute('text'));
    else if (['inlineCard', 'blockCard', 'embedCard'].includes(kind)) append(attribute('url'));
    else if (kind === 'rule') append('---');
    else if (['paragraph', 'heading', 'codeBlock'].includes(kind)) children.forEach(child => node(child, depth + 1));
    else if (['bulletList', 'orderedList'].includes(kind)) children.forEach((child, index) => { if (index) append('\n'); append('- '); if (isRecord(child)) blocks(child.content === null || child.content === undefined ? [] : requiredArray(child.content), '\n', depth); });
    else if (!['media', 'mediaSingle', 'mediaGroup', 'mediaInline'].includes(kind)) blocks(children, kind === 'tableRow' ? ' | ' : '\n', depth);
  };
  const blocks = (items: unknown[], separator: string, depth: number): void => { items.forEach((item, index) => { if (index) append(separator); node(item, depth + 1); }); };
  blocks(value.content === null || value.content === undefined ? [] : requiredArray(value.content), '\n\n', 0);
  return { text: text.replace(/\n+$/, ''), truncated };
}

/** Read a request, its first comments, and its SLAs. */
export class GetTicketOperation implements Query<GetTicketInput, TicketDetails> {
  /** Bind the shared client. */ constructor(private readonly client: JiraServiceManagementClient) {}
  /** Return the manifest definition. */ definition() { return GetTicketDefinition; }
  /** Perform the three reads needed for a complete request view. */
  async invoke(call: Call, input: GetTicketInput): Promise<QueryAttempt<TicketDetails>> {
    let issue: string;
    try { issue = trimID(input.issueIdOrKey, 'issueIdOrKey', new RegExp(`(?:${numericID.source})|(?:${issueKey.source})`)); }
    catch (error) { return queryDefect('getTicket', zeroDetails, error); }
    const commentLimit = input.commentLimit || 10;
    if (commentLimit < 1 || commentLimit > 50) return queryDefect('getTicket', zeroDetails, new TypeError('commentLimit must be between 1 and 50'));
    const session = await this.client.start(call, 'getTicket');
    if ('kind' in session) return queryClassified(session, zeroDetails);
    let result = await this.client.exchange(call, session, { method: 'GET', api: 'platform', path: issuePath(issue), query: { fields: [...ticketFields, 'description'].join(',') } });
    let classification = this.client.read(call, 'getTicket', 'request', result, issue);
    if (classification.kind !== 'success') return queryClassified(classification, zeroDetails);
    const details: TicketDetails = { ticket: zeroTicket, comments: [], slas: [] };
    try { details.ticket = decodeTicket(parsedObject(result), true); }
    catch { return invalid('getTicket', zeroDetails, classification.receipt); }
    const receipt = { ...classification.receipt, providerObjectId: details.ticket.key };
    result = await this.client.exchange(call, session, { method: 'GET', api: 'desk', path: `${requestPath(details.ticket.key)}/comment`, query: { public: 'true', internal: 'true', start: '0', limit: String(commentLimit) } });
    classification = this.client.read(call, 'getTicket', 'request comments', result, details.ticket.key);
    if (classification.kind !== 'success') return queryClassified({ ...classification, receipt }, zeroDetails);
    try {
      const page = serviceDeskPage(result), values = page.values;
      details.comments = values.map(convertComment);
      if (details.comments.length > commentLimit) throw new TypeError('too many comments');
      if (page.isLastPage === false) details.hasMoreComments = true;
    } catch { return invalid('getTicket', zeroDetails, receipt); }
    result = await this.client.exchange(call, session, { method: 'GET', api: 'desk', path: `${requestPath(details.ticket.key)}/sla`, query: { start: '0', limit: '50' } });
    classification = this.client.read(call, 'getTicket', 'request SLAs', result, details.ticket.key);
    if (classification.kind !== 'success') return queryClassified({ ...classification, receipt }, zeroDetails);
    try { details.slas = serviceDeskPage(result).values.map(convertSLA); }
    catch { return invalid('getTicket', zeroDetails, receipt); }
    return newQueryBranch('found', details, receipt);
  }
}

function providerDate(value: unknown): string | undefined {
  if (!isRecord(value)) return undefined;
  if (value.epochMillis !== undefined && value.epochMillis !== null) {
    if (!Number.isSafeInteger(value.epochMillis)) throw new TypeError('date epoch invalid');
    return new Date(Number(value.epochMillis)).toISOString().replace(/\.000Z$/, 'Z').replace(/(\.\d*?[1-9])0+Z$/, '$1Z');
  }
  const text = optionalString(value.iso8601);
  if (!text) return undefined;
  return jiraTime(text);
}
function convertComment(value: unknown): NonNullable<TicketDetails['comments']>[number] {
  if (!isRecord(value) || !/^[!-~]{1,128}$/.test(requiredString(value.id))) throw new TypeError('comment ID invalid');
  const createdAt = providerDate(value.created);
  if (!createdAt) throw new TypeError('comment date invalid');
  const body = optionalString(value.body);
  const comment: NonNullable<TicketDetails['comments']>[number] = { id: requiredString(value.id), isPublic: requiredBoolean(value.public), body: [...body].slice(0, 32767).join(''), createdAt };
  if ([...body].length > 32767) comment.isBodyTruncated = true;
  if (isRecord(value.author) && accountID.test(optionalString(value.author.accountId))) comment.author = { accountId: requiredString(value.author.accountId), ...(value.author.displayName ? { displayName: requiredString(value.author.displayName) } : {}) };
  return comment;
}
function convertSLA(value: unknown): NonNullable<TicketDetails['slas']>[number] {
  if (!isRecord(value) || !/^[!-~]{1,128}$/.test(requiredString(value.id))) throw new TypeError('SLA invalid');
  const sla: NonNullable<TicketDetails['slas']>[number] = { id: requiredString(value.id), name: optionalString(value.name) };
  if (isRecord(value.ongoingCycle)) sla.ongoingCycle = convertCycle(value.ongoingCycle, true);
  if (value.completedCycles !== undefined && value.completedCycles !== null) {
    const completed = requiredArray(value.completedCycles).slice(0, 20).map(cycle => convertCycle(cycle, false));
    if (completed.length) sla.completedCycles = completed;
  }
  return sla;
}
function convertCycle(value: unknown, ongoing: boolean): NonNullable<NonNullable<TicketDetails['slas']>[number]['ongoingCycle']> {
  if (!isRecord(value)) throw new TypeError('SLA cycle invalid');
  const startedAt = providerDate(value.startTime);
  if (!startedAt) throw new TypeError('SLA start invalid');
  const duration = (field: string): number => { const item = value[field]; if (item === undefined || item === null) return 0; if (!isRecord(item) || !Number.isSafeInteger(item.millis)) throw new TypeError('SLA duration invalid'); return Number(item.millis); };
  return { startedAt, ...(providerDate(value.breachTime) ? { breachAt: providerDate(value.breachTime) } : {}), ...(!ongoing && providerDate(value.stopTime) ? { stoppedAt: providerDate(value.stopTime) } : {}), isBreached: value.breached === undefined ? false : requiredBoolean(value.breached), ...(value.paused ? { isPaused: requiredBoolean(value.paused) } : {}), ...(value.withinCalendarHours ? { isWithinCalendarHours: requiredBoolean(value.withinCalendarHours) } : {}), goalDurationMillis: duration('goalDuration'), elapsedMillis: duration('elapsedTime'), remainingMillis: duration('remainingTime') };
}

type DispatchMarker = { jiraServiceManagementDispatchedCallId: string; attempt: number; canResend?: boolean };
async function claimSingleDispatch(call: Call): Promise<'granted' | 'alreadySent' | 'notRecorded'> {
  let marker: DispatchMarker | undefined;
  try {
    if (call.context.hasLastHeartbeatValue()) {
      const previous: unknown = call.context.getLastHeartbeatValue();
      if (isRecord(previous) && typeof previous.callId === 'string' && typeof previous.attempt === 'number' && typeof previous.state === 'string') marker = {
        jiraServiceManagementDispatchedCallId: previous.callId,
        attempt: previous.attempt,
        canResend: previous.state === 'canResend' || previous.state === 'notSent',
      };
      else marker = previous as DispatchMarker;
    }
  } catch { return 'alreadySent'; }
  const attempt = call.context.attempt;
  if (attempt < 1 || (attempt === 1 && marker !== undefined) || (attempt > 1 && (!marker || marker.jiraServiceManagementDispatchedCallId !== call.id || marker.attempt !== attempt - 1 || !marker.canResend))) return 'alreadySent';
  try { await call.context.recordHeartbeat({ jiraServiceManagementDispatchedCallId: call.id, attempt }); return 'granted'; }
  catch { return 'notRecorded'; }
}
async function releaseSingleDispatch(call: Call): Promise<void> {
  try { await call.context.recordHeartbeat({ jiraServiceManagementDispatchedCallId: call.id, attempt: call.context.attempt, canResend: true }); }
  catch { /* A lost release closes the next attempt, preserving single dispatch. */ }
}
function preSendAttempt<T>(claim: 'granted' | 'alreadySent' | 'notRecorded', operation: string, output: T, receipt: ReceiptDraft): MutationAttempt<T> | undefined {
  if (claim === 'notRecorded') return newMutationRetry(failure(operation, 'AVAILABILITY', 'Dex did not record the dispatch checkpoint; nothing was sent'));
  if (claim === 'alreadySent') return newMutationUncertain(output, failure(operation, 'TRANSPORT', 'an earlier attempt may have sent the request'), receipt);
  return undefined;
}
async function writeAttempt<T>(call: Call, classification: Classification, output: T, notFoundBranch = 'providerRejected'): Promise<MutationAttempt<T> | undefined> {
  if (classification.kind === 'success') return undefined;
  if (classification.kind === 'retry') { await releaseSingleDispatch(call); return newMutationRetry(classification.failure!, classification.retryAfterMs); }
  if (classification.kind === 'uncertain') return newMutationUncertain(output, classification.failure!, classification.receipt);
  if (classification.kind === 'defect') await releaseSingleDispatch(call);
  return mutationClassified(classification, output, notFoundBranch);
}
function validatePlainText(value: string, name: string, required: boolean): void {
  if (required && !value.trim()) throw new TypeError(`${name} is required`);
  if ([...value].length > 32767 || hasControl(value, true)) throw new TypeError(`${name} is invalid`);
}
function safeRejection(body: Uint8Array): { rejectedFieldIds?: string[]; providerErrorKey?: string } {
  try {
    const parsed: unknown = JSON.parse(Buffer.from(body).toString('utf8'));
    if (!isRecord(parsed)) return {};
    const ids = isRecord(parsed.errors) ? Object.keys(parsed.errors).filter(id => fieldID.test(id)).sort().slice(0, 20) : [];
    const key = isRecord(parsed.i18nErrorMessage) && typeof parsed.i18nErrorMessage.i18nKey === 'string' && /^[A-Za-z][A-Za-z0-9_.-]{0,127}$/.test(parsed.i18nErrorMessage.i18nKey) ? parsed.i18nErrorMessage.i18nKey : '';
    return { ...(ids.length ? { rejectedFieldIds: ids } : {}), ...(key ? { providerErrorKey: key } : {}) };
  } catch { return {}; }
}

/** Create a customer request with a persisted single-dispatch checkpoint. */
export class CreateTicketOperation implements Mutation<CreateTicketInput, CreateTicketOutput> {
  /** Bind the shared client. */ constructor(private readonly client: JiraServiceManagementClient) {}
  /** Return the manifest definition. */ definition() { return CreateTicketDefinition; }
  /** Derive the stable receipt key. */ idempotencyKey(callID: string) { return callID; }
  /** Validate, checkpoint, and create at most once. */
  async invoke(call: Call, input: CreateTicketInput): Promise<MutationAttempt<CreateTicketOutput>> {
    const requested: CreateTicketOutput = { serviceDeskId: input.serviceDeskId.trim(), requestTypeId: input.requestTypeId.trim(), summary: input.summary.trim(), ...(input.raiseOnBehalfOfAccountId?.trim() ? { raiseOnBehalfOfAccountId: input.raiseOnBehalfOfAccountId.trim() } : {}) };
    let payload: Record<string, unknown>;
    try {
      trimID(requested.serviceDeskId, 'serviceDeskId'); trimID(requested.requestTypeId, 'requestTypeId');
      if (!requested.summary || [...requested.summary].length > 255 || hasControl(requested.summary)) throw new TypeError('summary invalid');
      if (requested.raiseOnBehalfOfAccountId && !accountID.test(requested.raiseOnBehalfOfAccountId)) throw new TypeError('raiseOnBehalfOfAccountId invalid');
      validatePlainText(input.description || '', 'description', false);
      const values: Record<string, unknown> = { summary: requested.summary };
      if (input.description?.trim()) values.description = input.description;
      const extras = input.additionalFieldValues || {};
      if (Object.keys(extras).length > 20) throw new TypeError('too many additional fields');
      for (const [key, value] of Object.entries(extras)) { if (!fieldID.test(key) || key === 'summary' || key === 'description' || Buffer.byteLength(JSON.stringify(value)) > 16384) throw new TypeError('additional field invalid'); values[key] = value; }
      payload = { serviceDeskId: requested.serviceDeskId, requestTypeId: requested.requestTypeId, requestFieldValues: values, ...(requested.raiseOnBehalfOfAccountId ? { raiseOnBehalfOf: requested.raiseOnBehalfOfAccountId } : {}) };
    } catch (error) { return mutationDefect('createTicket', requested, error); }
    const claim = preSendAttempt(await claimSingleDispatch(call), 'createTicket', requested, this.client.receipt(call));
    if (claim) return claim;
    const session = await this.client.start(call, 'createTicket');
    if ('kind' in session) { if (session.kind === 'retry') await releaseSingleDispatch(call); return mutationClassified(session, requested); }
    const result = await this.client.exchange(call, session, { method: 'POST', api: 'desk', path: '/request', payload });
    const classification = this.client.write(call, 'createTicket', 'customer request', result);
    if (result.response?.status === 409) {
      const recovered = await this.readBack(call, session, result, requested);
      if (recovered) return newMutationBranch('created', recovered, { ...classification.receipt, providerObjectId: recovered.issueKey });
    }
    if (classification.kind === 'rejected') Object.assign(requested, safeRejection(result.response!.body));
    const terminal = await writeAttempt(call, classification, requested);
    if (terminal) return terminal;
    let created: Record<string, unknown>;
    try { created = parsedObject(result); } catch { return newMutationUncertain(requested, failure('createTicket', 'PROTOCOL', 'Jira accepted the request but returned an unusable reference'), classification.receipt); }
    if (!numericID.test(optionalString(created.issueId)) || !issueKey.test(optionalString(created.issueKey))) {
      const recovered = await this.readBack(call, session, result, requested);
      if (!recovered) return newMutationUncertain(requested, failure('createTicket', 'PROTOCOL', 'Jira accepted the request but its reference could not be confirmed'), classification.receipt);
      return newMutationBranch('created', recovered, { ...classification.receipt, providerObjectId: recovered.issueKey });
    }
    let output: CreateTicketOutput;
    try { output = createdTicketOutput(requested, created); }
    catch { return newMutationUncertain(requested, failure('createTicket', 'PROTOCOL', 'Jira accepted the request but returned an unusable response'), classification.receipt); }
    return newMutationBranch('created', output, { ...classification.receipt, providerObjectId: output.issueKey });
  }
  private async readBack(call: Call, session: { token: string; cloudID: string }, result: JiraResult, requested: CreateTicketOutput): Promise<CreateTicketOutput | undefined> {
    let accepted: Record<string, unknown>;
    try { accepted = parsedObject(result); } catch { return undefined; }
    const reference = numericID.test(optionalString(accepted.issueId)) ? requiredString(accepted.issueId) : optionalString(accepted.issueKey);
    if (!numericID.test(reference) && !issueKey.test(reference)) return undefined;
    const read = await this.client.exchange(call, session, { method: 'GET', api: 'desk', path: requestPath(reference) });
    if (this.client.read(call, 'createTicket', 'customer request', read).kind !== 'success') return undefined;
    try {
      const recovered = parsedObject(read);
      if (!numericID.test(requiredString(recovered.issueId)) || !issueKey.test(requiredString(recovered.issueKey)) || recovered.serviceDeskId !== requested.serviceDeskId || recovered.requestTypeId !== requested.requestTypeId || recovered.summary !== requested.summary) return undefined;
      if (requested.raiseOnBehalfOfAccountId && (!isRecord(recovered.reporter) || recovered.reporter.accountId !== requested.raiseOnBehalfOfAccountId)) return undefined;
      if (numericID.test(optionalString(accepted.issueId)) && accepted.issueId !== recovered.issueId || issueKey.test(optionalString(accepted.issueKey)) && accepted.issueKey !== recovered.issueKey) return undefined;
      return createdTicketOutput(requested, recovered);
    } catch { return undefined; }
  }
}
function createdTicketOutput(requested: CreateTicketOutput, created: Record<string, unknown>): CreateTicketOutput {
  const output: CreateTicketOutput = { ...requested, issueId: requiredString(created.issueId), issueKey: requiredString(created.issueKey) };
  const date = providerDate(created.createdDate);
  if (date) output.createdAt = date;
  if (isRecord(created.currentStatus)) output.requestStatus = { name: optionalString(created.currentStatus.status), ...(created.currentStatus.statusCategory ? { category: requiredString(created.currentStatus.statusCategory) } : {}) };
  return output;
}

/** Add a public reply or internal note with a persisted single-dispatch checkpoint. */
export class AddCommentOperation implements Mutation<AddCommentInput, AddCommentOutput> {
  /** Bind the shared client. */ constructor(private readonly client: JiraServiceManagementClient) {}
  /** Return the manifest definition. */ definition() { return AddCommentDefinition; }
  /** Derive the stable receipt key. */ idempotencyKey(callID: string) { return callID; }
  /** Validate, checkpoint, and add at most once. */
  async invoke(call: Call, input: AddCommentInput): Promise<MutationAttempt<AddCommentOutput>> {
    const requested: AddCommentOutput = { issueIdOrKey: input.issueIdOrKey, isPublic: input.isPublic ?? false };
    let issue: string;
    try { issue = trimID(input.issueIdOrKey, 'issueIdOrKey', new RegExp(`(?:${numericID.source})|(?:${issueKey.source})`)); requested.issueIdOrKey = issue; validatePlainText(input.body, 'body', true); }
    catch (error) { return mutationDefect('addComment', requested, error); }
    const claim = preSendAttempt(await claimSingleDispatch(call), 'addComment', requested, this.client.receipt(call));
    if (claim) return claim;
    const session = await this.client.start(call, 'addComment');
    if ('kind' in session) { if (session.kind === 'retry') await releaseSingleDispatch(call); return mutationClassified(session, requested); }
    const result = await this.client.exchange(call, session, { method: 'POST', api: 'desk', path: `${requestPath(issue)}/comment`, payload: { body: input.body, public: requested.isPublic } });
    const classification = this.client.write(call, 'addComment', 'comment', result, issue);
    if (result.response?.status === 409) {
      const recovered = await this.readBack(call, session, result, issue, input);
      if (recovered) return newMutationBranch('added', { ...requested, commentId: requiredString(recovered.id), isPublic: requiredBoolean(recovered.public), ...(providerDate(recovered.created) ? { createdAt: providerDate(recovered.created) } : {}) }, classification.receipt);
    }
    const terminal = await writeAttempt(call, classification, requested, 'notFound');
    if (terminal) return terminal;
    try {
      const created = parsedObject(result);
      if (!numericID.test(requiredString(created.id))) throw new TypeError('comment ID invalid');
      return newMutationBranch('added', { ...requested, commentId: requiredString(created.id), ...(created.public !== undefined && created.public !== null ? { isPublic: requiredBoolean(created.public) } : {}), ...(providerDate(created.created) ? { createdAt: providerDate(created.created) } : {}) }, classification.receipt);
    } catch { return newMutationUncertain(requested, failure('addComment', 'PROTOCOL', 'Jira accepted the comment but returned an unusable reference'), classification.receipt); }
  }
  private async readBack(call: Call, session: { token: string; cloudID: string }, result: JiraResult, issue: string, input: AddCommentInput): Promise<Record<string, unknown> | undefined> {
    let conflict: Record<string, unknown>;
    try { conflict = parsedObject(result); } catch { return undefined; }
    const id = optionalString(conflict.id);
    if (!numericID.test(id)) return undefined;
    const read = await this.client.exchange(call, session, { method: 'GET', api: 'desk', path: `${requestPath(issue)}/comment/${id}` });
    if (this.client.read(call, 'addComment', 'comment', read).kind !== 'success') return undefined;
    try { const recovered = parsedObject(read); if (recovered.id === id && recovered.body === input.body && recovered.public === (input.isPublic ?? false)) return recovered; }
    catch { /* An invalid read-back cannot confirm the write. */ }
    return undefined;
  }
}

/** Set labels and priority after reading the current issue. */
export class UpdateTicketOperation implements Mutation<UpdateTicketInput, UpdateTicketOutput> {
  /** Bind the shared client. */ constructor(private readonly client: JiraServiceManagementClient) {}
  /** Return the manifest definition. */ definition() { return UpdateTicketDefinition; }
  /** Derive the stable receipt key. */ idempotencyKey(callID: string) { return callID; }
  /** Read before writing so retries find changes already made. */
  async invoke(call: Call, input: UpdateTicketInput): Promise<MutationAttempt<UpdateTicketOutput>> {
    let issue: string;
    const zero: UpdateTicketOutput = { labels: [] };
    try {
      issue = trimID(input.issueIdOrKey, 'issueIdOrKey', new RegExp(`(?:${numericID.source})|(?:${issueKey.source})`));
      for (const [name, values] of [['addLabels', input.addLabels], ['removeLabels', input.removeLabels]] as const) {
        if ((values?.length ?? 0) > 50 || values?.some(value => !value || [...value].length > 255 || [...value].some(character => /\s/u.test(character) || hasControl(character) || [0x85, 0xa0, 0x2028, 0x2029].includes(character.codePointAt(0)!)))) throw new TypeError(`${name} invalid`);
      }
      if (input.addLabels?.some(value => input.removeLabels?.includes(value))) throw new TypeError('label added and removed');
      if (!input.addLabels?.length && !input.removeLabels?.length && !input.priorityName?.trim()) throw new TypeError('no change');
      if (input.priorityName?.trim()) validateSingleLine(input.priorityName, 'priorityName');
    } catch (error) { return mutationDefect('updateTicket', zero, error); }
    const session = await this.client.start(call, 'updateTicket');
    if ('kind' in session) return mutationClassified(session, zero);
    let result = await this.client.exchange(call, session, { method: 'GET', api: 'platform', path: issuePath(issue), query: { fields: 'labels,priority' } });
    let classification = this.client.read(call, 'updateTicket', 'request', result, issue);
    if (classification.kind !== 'success') return mutationClassified(classification, zero);
    let current: Record<string, unknown>;
    let fields: Record<string, unknown>;
    try {
      current = parsedObject(result);
      if (!numericID.test(requiredString(current.id)) || !issueKey.test(requiredString(current.key)) || !isRecord(current.fields)) throw new TypeError('issue invalid');
      fields = current.fields;
    } catch { return invalidMutation('updateTicket', zero, classification.receipt); }
    const key = requiredString(current.key), labels = fields.labels === undefined || fields.labels === null ? [] : requiredArray(fields.labels).map(requiredString);
    const output: UpdateTicketOutput = { issueId: requiredString(current.id), issueKey: key, labels: [...labels] };
    if (isRecord(fields.priority)) output.priority = { id: optionalString(fields.priority.id), name: optionalString(fields.priority.name) };
    const present = new Set(labels), removed = new Set<string>();
    const operations: Array<{ remove?: string; add?: string }> = [];
    for (const label of input.removeLabels || []) if (present.has(label) && !removed.has(label)) { operations.push({ remove: label }); removed.add(label); }
    const labelsAfter = labels.filter(label => !removed.has(label));
    for (const label of input.addLabels || []) if (!present.has(label)) { operations.push({ add: label }); present.add(label); labelsAfter.push(label); }
    const priority = input.priorityName?.trim() || '';
    const hasPriorityChange = priority && output.priority?.name.toLowerCase() !== priority.toLowerCase();
    const payload: Record<string, unknown> = { ...(operations.length ? { update: { labels: operations } } : {}), ...(hasPriorityChange ? { fields: { priority: { name: priority } } } : {}) };
    const receipt = { ...classification.receipt, providerObjectId: key };
    if (!Object.keys(payload).length) return newMutationBranch('updated', { ...output, wasAlreadyApplied: true }, receipt);
    result = await this.client.exchange(call, session, { method: 'PUT', api: 'platform', path: issuePath(key), payload });
    classification = this.client.read(call, 'updateTicket', 'request update', result, key);
    if (classification.kind !== 'success') {
      if (classification.kind === 'rejected') output.rejectedFieldIds = safeRejection(result.response!.body).rejectedFieldIds;
      return mutationClassified(classification, output);
    }
    output.labels = labelsAfter;
    if (hasPriorityChange) output.priority = { id: '', name: priority };
    return newMutationBranch('updated', output, { ...classification.receipt, providerObjectId: key });
  }
}

/** Move an issue through one available Jira workflow transition. */
export class TransitionTicketOperation implements Mutation<TransitionTicketInput, TransitionTicketOutput> {
  /** Bind the shared client. */ constructor(private readonly client: JiraServiceManagementClient) {}
  /** Return the manifest definition. */ definition() { return TransitionTicketDefinition; }
  /** Derive the stable receipt key. */ idempotencyKey(callID: string) { return callID; }
  /** Read the status and transitions before and after an uncertain write. */
  async invoke(call: Call, input: TransitionTicketInput): Promise<MutationAttempt<TransitionTicketOutput>> {
    let issue: string;
    try {
      issue = trimID(input.issueIdOrKey, 'issueIdOrKey', new RegExp(`(?:${numericID.source})|(?:${issueKey.source})`));
      if (input.transitionId?.trim() && !numericID.test(input.transitionId.trim())) throw new TypeError('transitionId invalid');
      for (const name of ['transitionName', 'destinationStatusName', 'resolutionName'] as const) if (input[name]?.trim()) validateSingleLine(input[name]!, name);
      if (!input.transitionId?.trim() && !input.transitionName?.trim() && !input.destinationStatusName?.trim()) throw new TypeError('no transition selector');
    } catch (error) { return mutationDefect('transitionTicket', zeroTransition, error); }
    const session = await this.client.start(call, 'transitionTicket');
    if ('kind' in session) return mutationClassified(session, zeroTransition);
    const first = await this.readSnapshot(call, session, issue);
    if ('kind' in first) return mutationClassified(first, zeroTransition);
    const { snapshot, receipt } = first;
    const output: TransitionTicketOutput = { issueId: snapshot.id, issueKey: snapshot.key, previousStatus: snapshot.status, status: snapshot.status };
    const destination = input.destinationStatusName?.trim().toLowerCase() || '';
    if (destination && snapshot.status.name.toLowerCase() === destination) return newMutationBranch('transitioned', { ...output, isAlreadyInDestinationStatus: true }, receipt);
    const matches = snapshot.transitions.filter(item => (!input.transitionId?.trim() || item.id === input.transitionId.trim()) && (!input.transitionName?.trim() || item.name.trim().toLowerCase() === input.transitionName.trim().toLowerCase()) && (!destination || item.destinationStatus.name.toLowerCase() === destination));
    if (matches.length !== 1) return newMutationBranch('transitionUnavailable', { ...output, ...(snapshot.transitions.length ? { availableTransitions: snapshot.transitions } : {}) }, receipt, failure('transitionTicket', matches.length ? 'VALIDATION' : 'CONFLICT', 'no unique transition matches the request'));
    const chosen = matches[0]!;
    output.transition = chosen;
    if (chosen.destinationStatus.id === snapshot.status.id) return newMutationBranch('transitioned', { ...output, isAlreadyInDestinationStatus: true }, receipt);
    const payload: Record<string, unknown> = { transition: { id: chosen.id }, ...(input.resolutionName?.trim() ? { fields: { resolution: { name: input.resolutionName.trim() } } } : {}) };
    const result = await this.client.exchange(call, session, { method: 'POST', api: 'platform', path: `${issuePath(issue)}/transitions`, payload });
    const writeReceipt = this.client.receipt(call, result.response, snapshot.key);
    if (!result.error && result.response!.status >= 200 && result.response!.status < 300) return newMutationBranch('transitioned', { ...output, status: chosen.destinationStatus }, writeReceipt);
    if (!result.error && result.response!.status === 404) return newMutationBranch('notFound', output, writeReceipt, failure('transitionTicket', 'NOT_FOUND', 'Jira issue was not found'));
    if (!result.error && result.response!.status === 429) return newMutationRetry(failure('transitionTicket', 'RATE_LIMIT', 'Jira rate limited the transition'));
    if (result.error && result.isProvablyNotSent) return newMutationRetry(failure('transitionTicket', 'TRANSPORT', 'Jira could not be reached'));
    const reread = await this.readSnapshot(call, session, issue);
    if ('kind' in reread) return newMutationRetry(failure('transitionTicket', 'AVAILABILITY', 'Jira transition outcome could not be read back'));
    if (reread.snapshot.status.id === chosen.destinationStatus.id) return newMutationBranch('transitioned', { ...output, status: reread.snapshot.status }, writeReceipt);
    if (!result.error && [400, 401, 403, 405, 410, 413, 415, 422].includes(result.response!.status)) return newMutationBranch('providerRejected', { ...output, status: reread.snapshot.status, ...safeRejection(result.response!.body) }, writeReceipt, failure('transitionTicket', result.response!.status === 401 ? 'AUTHENTICATION' : result.response!.status === 403 ? 'AUTHORIZATION' : [400, 422].includes(result.response!.status) ? 'VALIDATION' : 'PROVIDER_REJECTION', 'Jira rejected the transition'));
    if (!result.error && result.response!.status < 400) return invalidMutation('transitionTicket', output, writeReceipt);
    return newMutationRetry(failure('transitionTicket', result.response?.status === 409 ? 'CONFLICT' : 'AVAILABILITY', 'Jira did not confirm the transition'));
  }
  private async readSnapshot(call: Call, session: { token: string; cloudID: string }, issue: string): Promise<{ snapshot: { id: string; key: string; status: TicketStatus; transitions: NonNullable<TransitionTicketOutput['availableTransitions']> }; receipt: ReceiptDraft } | Classification> {
    const result = await this.client.exchange(call, session, { method: 'GET', api: 'platform', path: issuePath(issue), query: { fields: 'status', expand: 'transitions' } });
    const classification = this.client.read(call, 'transitionTicket', 'request', result, issue);
    if (classification.kind !== 'success') return classification;
    try {
      const resource = parsedObject(result);
      const id = requiredString(resource.id), key = requiredString(resource.key);
      if (!numericID.test(id) || !issueKey.test(key) || !isRecord(resource.fields) || !isRecord(resource.fields.status)) throw new TypeError('transition issue invalid');
      const status = statusView(resource.fields.status);
      const transitions = (resource.transitions === undefined || resource.transitions === null ? [] : requiredArray(resource.transitions)).flatMap(value => {
        if (!isRecord(value) || !value.id || !isRecord(value.to)) return [];
        return [{ id: requiredString(value.id), name: optionalString(value.name), destinationStatus: statusView(value.to) }];
      }).slice(0, 50);
      return { snapshot: { id, key, status, transitions }, receipt: { ...classification.receipt, providerObjectId: key } };
    } catch { return this.client.classification('invalidResponse', 'transitionTicket', 'PROTOCOL', 'Jira returned an invalid issue or transition list', classification.receipt); }
  }
}
function statusView(value: Record<string, unknown>): TicketStatus {
  const category = isRecord(value.statusCategory) ? value.statusCategory : {};
  return { id: optionalString(value.id), name: optionalString(value.name), ...(category.key ? { categoryKey: requiredString(category.key) } : {}) };
}
function validateSingleLine(value: string, name: string): void {
  const trimmed = value.trim();
  if (!trimmed || [...trimmed].length > 255 || hasControl(trimmed)) throw new TypeError(`${name} invalid`);
}

function hasControl(value: string, allowLineBreak = false): boolean { return [...value].some(character => { const code = character.codePointAt(0)!; return code === 127 || code < 32 && !(allowLineBreak && [9, 10, 13].includes(code)); }); }
