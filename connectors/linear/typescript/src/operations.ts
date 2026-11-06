// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { newMutationBranch, newMutationRetry, newQueryBranch, newQueryRetry, type Call, type Failure, type Mutation, type MutationAttempt, type Query, type QueryAttempt, type ReceiptDraft } from '@superdurable/connectors-sdk';
import type { LinearClient, LinearExchange } from './client.js';
import { AddCommentDefinition, CreateIssueDefinition, FindUserByEmailDefinition, GetIssueDefinition, ListWorkflowStatesDefinition, SearchIssuesDefinition, UpdateIssueDefinition } from './generated/definitions.js';
import type { AddCommentInput, AddCommentOutput, CreateIssueInput, CreateIssueOutput, FindUserByEmailInput, GetIssueInput, Issue, IssueSearchFilter, IssueSummary, ListWorkflowStatesInput, ListWorkflowStatesOutput, SearchIssuesInput, SearchIssuesOutput, UpdateIssueInput, UpdateIssueOutput, User, WorkflowState } from './generated/models.js';
import { addCommentDocument, createIssueDocument, findUserByEmailDocument, getIssueDocument, listWorkflowStatesDocument, readCommentDocument, readIssueSummaryDocument, searchIssuesDocument, updateIssueDocument } from './documents.js';

const zeroTime = '0001-01-01T00:00:00Z';
const zeroSummary: IssueSummary = { id: '', identifier: '', title: '', url: '', team: { id: '', key: '', name: '' }, state: { id: '', name: '', type: '' }, priority: 0, createdAt: zeroTime, updatedAt: zeroTime };
const zeroIssue: Issue = { ...zeroSummary, number: 0 };
const zeroUser: User = { id: '', name: '', displayName: '', email: '', active: false };
const uuidPattern = /^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$/;
const stateOrder = ['triage', 'backlog', 'unstarted', 'started', 'completed', 'canceled', 'duplicate'];

/** Search one bounded Linear issue page. */
export class SearchIssuesOperation implements Query<SearchIssuesInput, SearchIssuesOutput> {
  /** Bind the Linear client. */ constructor(private readonly client: LinearClient) {}
  /** Return the manifest definition. */ definition() { return SearchIssuesDefinition; }
  /** Validate and execute one page request. */
  async invoke(call: Call, input: SearchIssuesInput): Promise<QueryAttempt<SearchIssuesOutput>> {
    const empty = { issues: [] };
    let variables: Record<string, unknown>;
    try { variables = searchVariables(input); } catch (error) { return queryFailure('searchIssues', 'defect', empty, 'VALIDATION', String(error), {}); }
    const opened = await this.client.open(call, 'searchIssues');
    if ('failed' in opened) return queryExchange(opened.failed, empty, credentialReceipt(call, false));
    const exchange = await this.client.exchange(call, 'searchIssues', opened.credentials, 'LinearSearchIssues', searchIssuesDocument, variables);
    const receipt = this.client.receipt(call, exchange);
    if (exchange.disposition !== 'succeeded') return queryExchange(exchange, empty, receipt);
    try {
      const issues = object(exchange.data?.issues);
      const nodes = array(issues.nodes);
      if (nodes.length > 100) throw new Error('invalid issue page');
      const output: SearchIssuesOutput = { issues: nodes.map(node => decodeSummary(object(node))) };
      const pageInfo = object(issues.pageInfo);
      if (pageInfo.hasNextPage === true) {
        if (typeof pageInfo.endCursor !== 'string' || !cursorPattern.test(pageInfo.endCursor)) throw new Error('invalid cursor');
        output.nextCursor = pageInfo.endCursor;
      } else if (typeof pageInfo.hasNextPage !== 'boolean') throw new Error('invalid pageInfo');
      return newQueryBranch('searched', output, receipt);
    } catch { return queryFailure('searchIssues', 'invalidResponse', empty, 'PROTOCOL', 'Linear returned a malformed issue page', receipt); }
  }
}

/** Read one Linear issue. */
export class GetIssueOperation implements Query<GetIssueInput, Issue> {
  /** Bind the Linear client. */ constructor(private readonly client: LinearClient) {}
  /** Return the manifest definition. */ definition() { return GetIssueDefinition; }
  /** Validate and execute one issue request. */
  async invoke(call: Call, input: GetIssueInput): Promise<QueryAttempt<Issue>> {
    let locator: Locator;
    try { locator = parseLocator(input.issueId); } catch (error) { return queryFailure('getIssue', 'defect', zeroIssue, 'VALIDATION', String(error), {}); }
    const opened = await this.client.open(call, 'getIssue');
    if ('failed' in opened) return queryExchange(opened.failed, zeroIssue, credentialReceipt(call, false));
    const exchange = await this.client.exchange(call, 'getIssue', opened.credentials, 'LinearGetIssue', getIssueDocument, { filter: locator.filter });
    const receipt = this.client.receipt(call, exchange, locator.id);
    if (exchange.disposition !== 'succeeded') return queryExchange(exchange, zeroIssue, receipt);
    try {
      const nodes = issueNodes(exchange);
      if (nodes.length === 0) return queryFailure('getIssue', 'notFound', zeroIssue, 'NOT_FOUND', 'Linear has no issue with this ID that the connection can see', receipt);
      const issue = decodeIssue(object(nodes[0]));
      if (!locator.matches(issue)) throw new Error('issue mismatch');
      return newQueryBranch('found', issue, this.client.receipt(call, exchange, issue.id));
    } catch { return queryFailure('getIssue', 'invalidResponse', zeroIssue, 'PROTOCOL', 'Linear returned a malformed issue or another issue than requested', receipt); }
  }
}

/** Find one Linear user by email. */
export class FindUserByEmailOperation implements Query<FindUserByEmailInput, User> {
  /** Bind the Linear client. */ constructor(private readonly client: LinearClient) {}
  /** Return the manifest definition. */ definition() { return FindUserByEmailDefinition; }
  /** Validate and execute the user lookup. */
  async invoke(call: Call, input: FindUserByEmailInput): Promise<QueryAttempt<User>> {
    let email: string;
    try { email = validateEmail(input.email); } catch (error) { return queryFailure('findUserByEmail', 'defect', zeroUser, 'VALIDATION', String(error), {}); }
    const opened = await this.client.open(call, 'findUserByEmail');
    if ('failed' in opened) return queryExchange(opened.failed, zeroUser, credentialReceipt(call, false));
    const exchange = await this.client.exchange(call, 'findUserByEmail', opened.credentials, 'LinearFindUserByEmail', findUserByEmailDocument, { email, includeDisabled: input.includesDisabled ?? false });
    const receipt = this.client.receipt(call, exchange);
    if (exchange.disposition !== 'succeeded') return queryExchange(exchange, zeroUser, receipt);
    try {
      const nodes = array(object(exchange.data?.users).nodes);
      if (nodes.length > 1) throw new Error('multiple users');
      if (nodes.length === 0) return queryFailure('findUserByEmail', 'notFound', zeroUser, 'NOT_FOUND', 'no Linear user that the connection can see has this email', receipt);
      const user = object(nodes[0]);
      if (!uuidPattern.test(string(user.id)) || string(user.email).trim().toLowerCase() !== email.toLowerCase()) throw new Error('user mismatch');
      const output: User = { id: string(user.id), name: optionalString(user.name), displayName: optionalString(user.displayName), email: string(user.email), active: boolean(user.active), ...(user.admin ? { admin: boolean(user.admin) } : {}), ...(user.guest ? { guest: boolean(user.guest) } : {}), ...(user.url ? { url: string(user.url) } : {}) };
      return newQueryBranch('found', output, this.client.receipt(call, exchange, output.id));
    } catch { return queryFailure('findUserByEmail', 'invalidResponse', zeroUser, 'PROTOCOL', 'Linear returned a malformed user list', receipt); }
  }
}

/** List one team's workflow states. */
export class ListWorkflowStatesOperation implements Query<ListWorkflowStatesInput, ListWorkflowStatesOutput> {
  /** Bind the Linear client. */ constructor(private readonly client: LinearClient) {}
  /** Return the manifest definition. */ definition() { return ListWorkflowStatesDefinition; }
  /** Validate and execute one team state request. */
  async invoke(call: Call, input: ListWorkflowStatesInput): Promise<QueryAttempt<ListWorkflowStatesOutput>> {
    const requested: ListWorkflowStatesOutput = { teamId: input.teamId, states: [] };
    let teamId: string;
    try { teamId = validateUUID(input.teamId, 'teamId', true); requested.teamId = teamId; } catch (error) { return queryFailure('listWorkflowStates', 'defect', requested, 'VALIDATION', String(error), {}); }
    const opened = await this.client.open(call, 'listWorkflowStates');
    if ('failed' in opened) return queryExchange(opened.failed, requested, credentialReceipt(call, false));
    const exchange = await this.client.exchange(call, 'listWorkflowStates', opened.credentials, 'LinearListWorkflowStates', listWorkflowStatesDocument, { teamId });
    const receipt = this.client.receipt(call, exchange, teamId);
    if (exchange.disposition !== 'succeeded') return queryExchange(exchange, requested, receipt);
    try {
      const nodes = array(object(exchange.data?.workflowStates).nodes);
      if (nodes.length > 100) throw new Error('too many states');
      const states: WorkflowState[] = nodes.map(value => {
        const node = object(value);
        if (!uuidPattern.test(string(node.id)) || !string(node.name) || !string(node.type) || string(object(node.team).id).toLowerCase() !== teamId) throw new Error('invalid state');
        return { id: string(node.id), name: string(node.name), type: string(node.type), position: number(node.position), ...(node.color ? { color: string(node.color) } : {}) };
      });
      if (!states.length) return queryFailure('listWorkflowStates', 'notFound', requested, 'NOT_FOUND', 'Linear has no team with this ID that the connection can see', receipt);
      states.sort((left, right) => (stateOrder.indexOf(left.type) < 0 ? 8 : stateOrder.indexOf(left.type)) - (stateOrder.indexOf(right.type) < 0 ? 8 : stateOrder.indexOf(right.type)) || left.position - right.position);
      return newQueryBranch('listed', { teamId, states }, receipt);
    } catch { return queryFailure('listWorkflowStates', 'invalidResponse', requested, 'PROTOCOL', 'Linear returned a malformed workflow state list', receipt); }
  }
}

/** Create a Linear issue under a stable client supplied UUID. */
export class CreateIssueOperation implements Mutation<CreateIssueInput, CreateIssueOutput> {
  /** Bind the Linear client. */ constructor(private readonly client: LinearClient) {}
  /** Return the manifest definition. */ definition() { return CreateIssueDefinition; }
  /** Reuse the Dex Call ID for every attempt. */ idempotencyKey(callId: string) { return callId; }
  /** Send and read back one idempotent create. */
  async invoke(call: Call, input: CreateIssueInput): Promise<MutationAttempt<CreateIssueOutput>> {
    const requested: CreateIssueOutput = { teamId: input.teamId, title: input.title };
    let issueId: string, variables: Record<string, unknown>;
    try { ({ issueId, variables } = createVariables(call.idempotencyKey ?? '', input, requested)); }
    catch (error) { return mutationFailure('createIssue', 'defect', requested, 'VALIDATION', String(error), {}); }
    const opened = await this.client.open(call, 'createIssue');
    if ('failed' in opened) return writeExchange(opened.failed, requested, credentialReceipt(call, true));
    let exchange = await this.client.exchange(call, 'createIssue', opened.credentials, 'LinearCreateIssue', createIssueDocument, variables);
    const receipt = this.client.receipt(call, exchange, issueId);
    const decoded = mutationIssue(exchange, 'issueCreate');
    if (decoded) return newMutationBranch('created', { ...requested, issueId: decoded.id, issue: decoded }, this.client.receipt(call, exchange, decoded.id));
    exchange = classifyUnusableWrite(exchange, 'issueCreate', 'createIssue');
    if (readBackWorthy(exchange)) {
      const read = await readIssue(this.client, call, opened.credentials, 'createIssue', parseLocator(issueId));
      if (read.summary) return newMutationBranch('created', { ...requested, issueId: read.summary.id, issue: read.summary, replayed: true }, this.client.receipt(call, read.exchange, issueId));
      if (read.exchange.disposition !== 'succeeded') return newMutationRetry(read.exchange.failure!, read.exchange.retryAfterMs);
      if (exchange.disposition === 'invalid') return newMutationRetry(exchange.failure!, 0);
    }
    return writeExchange(exchange, requested, receipt);
  }
}

/** Update absolute issue fields and label sets. */
export class UpdateIssueOperation implements Mutation<UpdateIssueInput, UpdateIssueOutput> {
  /** Bind the Linear client. */ constructor(private readonly client: LinearClient) {}
  /** Return the manifest definition. */ definition() { return UpdateIssueDefinition; }
  /** Reuse the Dex Call ID for every attempt. */ idempotencyKey(callId: string) { return callId; }
  /** Send one repeat safe update. */
  async invoke(call: Call, input: UpdateIssueInput): Promise<MutationAttempt<UpdateIssueOutput>> {
    const requested: UpdateIssueOutput = { issueId: input.issueId };
    let locator: Locator, variables: Record<string, unknown>;
    try { ({ locator, variables } = updateVariables(input)); requested.issueId = locator.id; }
    catch (error) { return mutationFailure('updateIssue', 'defect', requested, 'VALIDATION', String(error), {}); }
    const opened = await this.client.open(call, 'updateIssue');
    if ('failed' in opened) return updateExchange(opened.failed, requested, credentialReceipt(call, true));
    let exchange = await this.client.exchange(call, 'updateIssue', opened.credentials, 'LinearUpdateIssue', updateIssueDocument, variables);
    const receipt = this.client.receipt(call, exchange, locator.id);
    const decoded = mutationIssue(exchange, 'issueUpdate');
    if (decoded) return newMutationBranch('updated', { ...requested, issue: decoded }, this.client.receipt(call, exchange, decoded.id));
    exchange = classifyUnusableWrite(exchange, 'issueUpdate', 'updateIssue');
    if (exchange.disposition === 'rejected' && readBackWorthy(exchange)) {
      const read = await readIssue(this.client, call, opened.credentials, 'updateIssue', locator);
      if (read.exchange.disposition === 'succeeded' && !read.summary) return mutationFailure('updateIssue', 'notFound', requested, 'NOT_FOUND', 'Linear has no issue with this ID that the connection can see', receipt);
      if (read.exchange.disposition === 'retry') return newMutationRetry(read.exchange.failure!, read.exchange.retryAfterMs);
    }
    return updateExchange(exchange, requested, receipt);
  }
}

/** Add a comment under a stable client supplied UUID. */
export class AddCommentOperation implements Mutation<AddCommentInput, AddCommentOutput> {
  /** Bind the Linear client. */ constructor(private readonly client: LinearClient) {}
  /** Return the manifest definition. */ definition() { return AddCommentDefinition; }
  /** Reuse the Dex Call ID for every attempt. */ idempotencyKey(callId: string) { return callId; }
  /** Send and read back one idempotent comment. */
  async invoke(call: Call, input: AddCommentInput): Promise<MutationAttempt<AddCommentOutput>> {
    const requested: AddCommentOutput = { issueId: input.issueId };
    let locator: Locator, commentId: string;
    try { locator = parseLocator(input.issueId); requested.issueId = locator.id; validateMarkdown(input.body, true); commentId = entityId(call.idempotencyKey ?? '', 'comment'); }
    catch (error) { return mutationFailure('addComment', 'defect', requested, 'VALIDATION', String(error), {}); }
    const opened = await this.client.open(call, 'addComment');
    if ('failed' in opened) return writeExchange(opened.failed, requested, credentialReceipt(call, true));
    let exchange = await this.client.exchange(call, 'addComment', opened.credentials, 'LinearAddComment', addCommentDocument, { input: { id: commentId, issueId: locator.id, body: input.body } });
    const receipt = this.client.receipt(call, exchange, commentId);
    const comment = mutationComment(exchange, commentId);
    if (comment) return newMutationBranch('added', { ...requested, commentId, url: comment.url, createdAt: comment.createdAt }, receipt);
    exchange = classifyUnusableWrite(exchange, 'commentCreate', 'addComment');
    if (!readBackWorthy(exchange)) return writeExchange(exchange, requested, receipt);
    const read = await this.client.exchange(call, 'addComment', opened.credentials, 'LinearReadComment', readCommentDocument, { filter: { id: { eq: commentId } } });
    if (read.disposition !== 'succeeded') return newMutationRetry(read.failure!, read.retryAfterMs);
    try {
      const nodes = array(object(read.data?.comments).nodes);
      if (nodes.length > 1) throw new Error('multiple comments');
      if (nodes.length === 1) {
        const recovered = decodeComment(object(nodes[0]), commentId);
        return newMutationBranch('added', { ...requested, commentId, url: recovered.url, createdAt: recovered.createdAt, replayed: true }, this.client.receipt(call, read, commentId));
      }
    } catch { return newMutationRetry(failure('addComment', 'PROTOCOL', 'Linear returned an unusable comment read-back')); }
    if (exchange.disposition !== 'rejected') return writeExchange(exchange, requested, receipt);
    const issueRead = await readIssue(this.client, call, opened.credentials, 'addComment', locator);
    if (issueRead.exchange.disposition === 'retry') return newMutationRetry(issueRead.exchange.failure!, issueRead.exchange.retryAfterMs);
    if (issueRead.exchange.disposition === 'succeeded' && !issueRead.summary) return mutationFailure('addComment', 'notFound', requested, 'NOT_FOUND', 'Linear has no issue with this ID that the connection can see', receipt);
    return writeExchange(exchange, requested, receipt);
  }
}

function failure(operation: string, kind: Failure['kind'], message: string): Failure { return { kind, provider: 'linear', operation, message }; }
function credentialReceipt(call: Call, isMutation: boolean): ReceiptDraft { return { callId: call.id, provider: 'linear', ...(isMutation && call.idempotencyKey ? { idempotencyKey: call.idempotencyKey } : {}) }; }
function queryFailure<T>(operation: string, branch: string, output: T, kind: Failure['kind'], message: string, receipt: ReceiptDraft): QueryAttempt<T> { return newQueryBranch(branch, output, receipt, failure(operation, kind, message)); }
function mutationFailure<T>(operation: string, branch: string, output: T, kind: Failure['kind'], message: string, receipt: ReceiptDraft): MutationAttempt<T> { return newMutationBranch(branch, output, receipt, failure(operation, kind, message)); }
function queryExchange<T>(exchange: LinearExchange, output: T, receipt: ReceiptDraft): QueryAttempt<T> {
  if (exchange.disposition === 'retry') return newQueryRetry(exchange.failure!, exchange.retryAfterMs);
  return newQueryBranch(exchange.disposition === 'invalid' ? 'invalidResponse' : exchange.disposition === 'defect' ? 'defect' : 'providerRejected', output, receipt, exchange.failure);
}
function writeExchange<T>(exchange: LinearExchange, output: T, receipt: ReceiptDraft): MutationAttempt<T> {
  if (exchange.disposition === 'retry' || exchange.disposition === 'invalid') return newMutationRetry(exchange.failure!, exchange.retryAfterMs);
  return newMutationBranch(exchange.disposition === 'defect' ? 'defect' : 'providerRejected', output, receipt, exchange.failure);
}
function updateExchange(exchange: LinearExchange, output: UpdateIssueOutput, receipt: ReceiptDraft): MutationAttempt<UpdateIssueOutput> {
  if (exchange.disposition === 'retry') return newMutationRetry(exchange.failure!, exchange.retryAfterMs);
  return newMutationBranch(exchange.disposition === 'invalid' ? 'invalidResponse' : exchange.disposition === 'defect' ? 'defect' : 'providerRejected', output, receipt, exchange.failure);
}
function readBackWorthy(exchange: LinearExchange): boolean {
  return exchange.disposition === 'invalid' || exchange.disposition === 'retry' && ['AVAILABILITY', 'TRANSPORT'].includes(exchange.failure?.kind ?? '') || exchange.disposition === 'rejected' && ['VALIDATION', 'PROVIDER_REJECTION', 'QUOTA_EXHAUSTED'].includes(exchange.failure?.kind ?? '');
}
function classifyUnusableWrite(exchange: LinearExchange, field: string, operation: string): LinearExchange {
  if (exchange.disposition !== 'succeeded') return exchange;
  const payload = exchange.data?.[field];
  if (payload && typeof payload === 'object' && (payload as Record<string, unknown>).success === false) return { ...exchange, disposition: 'rejected', failure: failure(operation, 'PROVIDER_REJECTION', 'Linear reported that the write did not succeed') };
  return { ...exchange, disposition: 'invalid', failure: failure(operation, 'PROTOCOL', 'Linear accepted the write but returned an unusable record') };
}
function mutationIssue(exchange: LinearExchange, field: string): IssueSummary | undefined {
  try { const payload = object(exchange.data?.[field]); return payload.success === true ? decodeSummary(object(payload.issue)) : undefined; } catch { return undefined; }
}
function mutationComment(exchange: LinearExchange, commentId: string): { url: string; createdAt: string } | undefined {
  try { const payload = object(exchange.data?.commentCreate); return payload.success === true ? decodeComment(object(payload.comment), commentId) : undefined; } catch { return undefined; }
}
function decodeComment(value: Record<string, unknown>, commentId: string): { url: string; createdAt: string } {
  if (string(value.id).toLowerCase() !== commentId.toLowerCase()) throw new Error('comment mismatch');
  return { url: optionalString(value.url), createdAt: parseTime(string(value.createdAt)) };
}
async function readIssue(client: LinearClient, call: Call, credentials: Parameters<LinearClient['exchange']>[2], operation: string, locator: Locator): Promise<{ exchange: LinearExchange; summary?: IssueSummary }> {
  const exchange = await client.exchange(call, operation, credentials, 'LinearReadIssueSummary', readIssueSummaryDocument, { filter: locator.filter });
  if (exchange.disposition !== 'succeeded') return { exchange };
  try {
    const nodes = issueNodes(exchange);
    if (!nodes.length) return { exchange };
    const summary = decodeSummary(object(nodes[0]));
    if (!locator.matches(summary)) throw new Error('issue mismatch');
    return { exchange, summary };
  } catch { return { exchange: { ...exchange, disposition: 'invalid', failure: failure(operation, 'PROTOCOL', 'Linear returned a malformed issue or another issue than requested') } }; }
}
function issueNodes(exchange: LinearExchange): unknown[] {
  const nodes = array(object(exchange.data?.issues).nodes);
  if (nodes.length > 1) throw new Error('multiple issues');
  return nodes;
}
function decodeSummary(value: Record<string, unknown>): IssueSummary {
  const team = object(value.team), state = object(value.state);
  if (!uuidPattern.test(string(value.id)) || !string(value.identifier).trim() || !uuidPattern.test(string(team.id)) || !uuidPattern.test(string(state.id))) throw new Error('invalid issue');
  const priority = number(value.priority);
  if (priority < 0 || priority > 4) throw new Error('invalid priority');
  const summary: IssueSummary = { id: string(value.id), identifier: string(value.identifier), title: optionalString(value.title), url: optionalString(value.url),
    team: { id: string(team.id), key: optionalString(team.key), name: optionalString(team.name) }, state: { id: string(state.id), name: optionalString(state.name), type: optionalString(state.type) },
    priority: Math.trunc(priority), createdAt: parseTime(string(value.createdAt)), updatedAt: parseTime(string(value.updatedAt)) };
  if (value.assignee != null) summary.assigneeId = optionalString(object(value.assignee).id);
  if (value.labelIds != null) { const labelIds = array(value.labelIds).map(string); if (labelIds.length) summary.labelIds = labelIds; }
  if (value.dueDate != null) summary.dueDate = string(value.dueDate);
  if (value.archivedAt != null) summary.archivedAt = parseTime(string(value.archivedAt));
  return summary;
}
function decodeIssue(value: Record<string, unknown>): Issue {
  const summary = decodeSummary(value);
  const wireNumber = number(value.number);
  if (wireNumber < 0 || wireNumber > 2_147_483_647) throw new Error('invalid issue number');
  const issue: Issue = { ...summary, number: Math.trunc(wireNumber) };
  if (value.priorityLabel) issue.priorityLabel = string(value.priorityLabel);
  if (value.estimate != null) issue.estimate = number(value.estimate);
  if (value.description != null) {
    const text = string(value.description);
    const characters = Array.from(text);
    issue.description = characters.slice(0, 32_768).join('');
    if (characters.length > 32_768) issue.descriptionTruncated = true;
  }
  if (value.branchName) issue.branchName = string(value.branchName);
  if (value.trashed === true) issue.trashed = true;
  for (const name of ['assignee', 'creator'] as const) if (value[name] != null) {
    const reference = object(value[name]);
    issue[name] = { id: string(reference.id), ...(reference.name ? { name: string(reference.name) } : {}), ...(reference.displayName ? { displayName: string(reference.displayName) } : {}) };
  }
  if (value.labels != null) {
    const labels = array(object(value.labels).nodes).map(node => { const label = object(node); return { id: string(label.id), name: string(label.name), ...(label.color ? { color: string(label.color) } : {}) }; });
    if (labels.length) issue.labels = labels;
  }
  if (value.project != null) { const project = object(value.project); issue.project = { id: string(project.id), name: string(project.name) }; }
  if (value.cycle != null) { const cycle = object(value.cycle); issue.cycle = { id: string(cycle.id), number: Math.trunc(number(cycle.number)), ...(cycle.name ? { name: string(cycle.name) } : {}) }; }
  if (value.parent != null) { const parent = object(value.parent); issue.parent = { id: string(parent.id), identifier: string(parent.identifier) }; }
  for (const name of ['startedAt', 'completedAt', 'canceledAt'] as const) if (value[name] != null) issue[name] = parseTime(string(value[name]));
  return issue;
}

interface Locator { id: string; filter: Record<string, unknown>; matches: (summary: IssueSummary) => boolean; }
function parseLocator(value: string): Locator {
  const trimmed = value.trim();
  if (uuidPattern.test(trimmed)) { const id = trimmed.toLowerCase(); return { id, filter: { id: { eq: id } }, matches: summary => summary.id.toLowerCase() === id }; }
  const match = /^([A-Za-z][A-Za-z0-9]{0,9})-([1-9][0-9]{0,8})$/.exec(trimmed);
  if (!match) throw new Error('issueId must be an issue UUID or an identifier such as ENG-123');
  const teamKey = match[1]!.toUpperCase(), number = Number(match[2]);
  const id = `${teamKey}-${number}`;
  return { id, filter: { team: { key: { eq: teamKey } }, number: { eq: number } }, matches: summary => summary.identifier.toLowerCase() === id.toLowerCase() };
}
function validateUUID(value: string | undefined, field: string, required = false): string {
  const trimmed = (value ?? '').trim();
  if (!trimmed && !required) return '';
  if (!uuidPattern.test(trimmed)) throw new Error(`${field} must be a Linear UUID`);
  return trimmed.toLowerCase();
}
function validateUUIDs(values: string[] | undefined, field: string): string[] {
  if ((values?.length ?? 0) > 50) throw new Error(`${field} accepts at most 50 IDs`);
  const ids = (values ?? []).map(value => validateUUID(value, field, true));
  if (new Set(ids).size !== ids.length) throw new Error(`${field} lists an ID twice`);
  return ids;
}
function validateText(value: string, field: string): string {
  const trimmed = value.trim();
  if (!trimmed || Array.from(trimmed).length > 255 || Array.from(trimmed).some(character => character.charCodeAt(0) < 32 || character.charCodeAt(0) === 127)) throw new Error(`${field} must be one line of up to 255 characters`);
  return trimmed;
}
function validateMarkdown(value: string | undefined, required: boolean): string {
  const text = value ?? '';
  if (required && !text.trim() || Array.from(text).length > 65_536 || Array.from(text).some(character => character.charCodeAt(0) < 32 && !['\n', '\r', '\t'].includes(character) || character.charCodeAt(0) === 127)) throw new Error('body or description is invalid Markdown');
  return text;
}
function validateEmail(value: string): string {
  const email = value.trim(), index = email.indexOf('@');
  if (!email || Buffer.byteLength(email) > 320 || index < 1 || !email.slice(index + 1).includes('.') || email.slice(index + 1).includes('@') || Array.from(email).some(character => character.charCodeAt(0) <= 32 || character.charCodeAt(0) === 127)) throw new Error('email must be one address such as alice@example.com');
  return email;
}
function validateDate(value: string | undefined): string {
  const date = (value ?? '').trim();
  if (!date) return '';
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(date);
  if (!match || Number(match[2]) < 1 || Number(match[2]) > 12 || Number(match[3]) < 1 || Number(match[3]) > daysInMonth(Number(match[1]), Number(match[2]))) throw new Error('dueDate must be a calendar date');
  return date;
}
function parseTime(value: string): string {
  const match = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:[.,](\d+))?(Z|[+-]\d{2}:\d{2})$/.exec(value);
  if (!match || Number(match[4]) > 23 || Number(match[5]) > 59 || Number(match[6]) > 59 || Number(match[2]) < 1 || Number(match[2]) > 12 || Number(match[3]) < 1 || Number(match[3]) > daysInMonth(Number(match[1]), Number(match[2]))) throw new Error('invalid Linear time');
  const offset = match[8] === 'Z' ? 0 : (match[8]!.startsWith('+') ? 1 : -1) * (Number(match[8]!.slice(1, 3)) * 60 + Number(match[8]!.slice(4, 6)));
  if (match[8] !== 'Z' && Number(match[8]!.slice(4, 6)) > 59) throw new Error('invalid Linear time');
  const parsed = new Date(0);
  parsed.setUTCFullYear(Number(match[1]), Number(match[2]) - 1, Number(match[3]));
  parsed.setUTCHours(Number(match[4]), Number(match[5]) - offset, Number(match[6]), 0);
  if (!Number.isFinite(parsed.getTime())) throw new Error('invalid Linear time');
  const fractional = (match[7] ?? '').slice(0, 9).padEnd(9, '0').replace(/0+$/, '');
  return parsed.toISOString().replace('.000Z', fractional ? `.${fractional}Z` : 'Z');
}
function daysInMonth(year: number, month: number): number {
  if (month === 2) return year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0) ? 29 : 28;
  return [4, 6, 9, 11].includes(month) ? 30 : 31;
}
function validatePriority(value: number | undefined): void { if (value !== undefined && (!Number.isInteger(value) || value < 0 || value > 4)) throw new Error('priority must be 0 through 4'); }
function entityId(key: string, entity: string): string {
  if (!key.trim()) throw new Error('Linear write has no idempotency key');
  const digest = createHash('sha256').update(`dex-linear-client-id/v1\0${entity}\0${key}`).digest();
  digest[6] = (digest[6]! & 0x0f) | 0x40;
  digest[8] = (digest[8]! & 0x3f) | 0x80;
  const hex = digest.subarray(0, 16).toString('hex');
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20, 32)}`;
}
function object(value: unknown): Record<string, unknown> { if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('expected object'); return value as Record<string, unknown>; }
function array(value: unknown): unknown[] { if (value == null) return []; if (!Array.isArray(value)) throw new Error('expected array'); return value; }
function string(value: unknown): string { if (value == null) return ''; if (typeof value !== 'string') throw new Error('expected string'); return value; }
function optionalString(value: unknown): string { return value == null ? '' : string(value); }
function number(value: unknown): number { if (value == null) return 0; if (typeof value !== 'number' || !Number.isFinite(value)) throw new Error('expected number'); return value; }
function boolean(value: unknown): boolean { if (value == null) return false; if (typeof value !== 'boolean') throw new Error('expected boolean'); return value; }

const cursorPattern = /^[A-Za-z0-9_\-.~=+/:]{1,512}$/;
function searchVariables(input: SearchIssuesInput): Record<string, unknown> {
  const pageSize = input.pageSize || 50;
  if (!Number.isInteger(pageSize) || pageSize < 1 || pageSize > 100) throw new Error('pageSize must be between 1 and 100');
  const order = input.order || 'createdAt';
  if (order !== 'createdAt' && order !== 'updatedAt') throw new Error('order must be createdAt or updatedAt');
  const filter = searchFilter(input.filter);
  const variables: Record<string, unknown> = { first: pageSize, orderBy: order, includeArchived: input.includesArchived ?? false };
  if (Object.keys(filter).length) variables.filter = filter;
  const cursor = input.cursor?.trim() ?? '';
  if (cursor) { if (!cursorPattern.test(cursor)) throw new Error('cursor is invalid'); variables.after = cursor; }
  return variables;
}
function searchFilter(filter: IssueSearchFilter): Record<string, unknown> {
  const clauses: Record<string, unknown> = {};
  const teamId = validateUUID(filter.teamId, 'filter.teamId'), teamKey = filter.teamKey?.trim() ?? '';
  if (teamId && teamKey) throw new Error('set at most one of filter.teamId and filter.teamKey');
  if (teamId) clauses.team = { id: { eq: teamId } };
  else if (teamKey) { if (!/^[A-Za-z][A-Za-z0-9]{0,9}$/.test(teamKey)) throw new Error('invalid team key'); clauses.team = { key: { eq: teamKey.toUpperCase() } }; }
  const state: Record<string, unknown> = {};
  if ((filter.stateTypes?.length ?? 0) > 20) throw new Error('too many state types');
  if (filter.stateTypes?.length) { if (filter.stateTypes.some(value => !stateOrder.includes(value))) throw new Error('unknown state type'); state.type = { in: filter.stateTypes }; }
  const stateIds = validateUUIDs(filter.stateIds, 'filter.stateIds');
  if (stateIds.length) state.id = { in: stateIds };
  if (Object.keys(state).length) clauses.state = state;
  const assigneeId = validateUUID(filter.assigneeId, 'filter.assigneeId');
  if (filter.unassigned && (assigneeId || filter.assigneeEmail)) throw new Error('unassigned excludes assignee');
  if (filter.unassigned) clauses.assignee = { null: true };
  else {
    const assignee: Record<string, unknown> = {};
    if (assigneeId) assignee.id = { eq: assigneeId };
    if (filter.assigneeEmail) assignee.email = { eqIgnoreCase: validateEmail(filter.assigneeEmail) };
    if (Object.keys(assignee).length) clauses.assignee = assignee;
  }
  const labels: Record<string, unknown> = {};
  const labelIds = validateUUIDs(filter.labelIds, 'filter.labelIds');
  if (labelIds.length) labels.id = { in: labelIds };
  if ((filter.labelNames?.length ?? 0) > 20) throw new Error('too many label names');
  if (filter.labelNames?.length) labels.name = { in: filter.labelNames.map(name => validateText(name, 'filter.labelNames')) };
  if (Object.keys(labels).length) clauses.labels = { some: labels };
  const projectId = validateUUID(filter.projectId, 'filter.projectId');
  if (projectId) clauses.project = { id: { eq: projectId } };
  const title: Record<string, unknown> = {};
  if (filter.title) title.eq = validateText(filter.title, 'filter.title');
  if (filter.titleContains) title.containsIgnoreCase = validateText(filter.titleContains, 'filter.titleContains');
  if (Object.keys(title).length) clauses.title = title;
  if (filter.updatedAfter) clauses.updatedAt = { gt: formatFilterTime(filter.updatedAfter) };
  if (filter.createdAfter) clauses.createdAt = { gt: formatFilterTime(filter.createdAfter) };
  return clauses;
}
function formatFilterTime(value: string): string { return new Date(parseTime(value)).toISOString(); }
function createVariables(key: string, input: CreateIssueInput, requested: CreateIssueOutput): { issueId: string; variables: Record<string, unknown> } {
  const teamId = validateUUID(input.teamId, 'teamId', true), title = validateText(input.title, 'title');
  requested.teamId = teamId; requested.title = title;
  const description = validateMarkdown(input.description, false);
  validatePriority(input.priority);
  const dueDate = validateDate(input.dueDate);
  const labelIds = validateUUIDs(input.labelIds, 'labelIds');
  const issueId = entityId(key, 'issue');
  const fields: Record<string, unknown> = { id: issueId, teamId, title };
  for (const name of ['stateId', 'assigneeId', 'projectId'] as const) { const value = validateUUID(input[name], name); if (value) fields[name] = value; }
  if (input.parentId) fields.parentId = parseLocator(input.parentId).id;
  if (description) fields.description = description;
  if (labelIds.length) fields.labelIds = labelIds;
  if (input.priority !== undefined) fields.priority = input.priority;
  if (dueDate) fields.dueDate = dueDate;
  return { issueId, variables: { input: fields } };
}
function updateVariables(input: UpdateIssueInput): { locator: Locator; variables: Record<string, unknown> } {
  const locator = parseLocator(input.issueId);
  const fields: Record<string, unknown> = {};
  const stateId = validateUUID(input.stateId, 'stateId'), assigneeId = validateUUID(input.assigneeId, 'assigneeId');
  if (stateId) fields.stateId = stateId;
  if (assigneeId && input.unassigns) throw new Error('set at most one of assigneeId and unassigns');
  if (assigneeId) fields.assigneeId = assigneeId;
  else if (input.unassigns) fields.assigneeId = null;
  const added = validateUUIDs(input.addedLabelIds, 'addedLabelIds'), removed = validateUUIDs(input.removedLabelIds, 'removedLabelIds');
  if (added.some(id => removed.includes(id))) throw new Error('a label cannot be both added and removed');
  if (added.length) fields.addedLabelIds = added;
  if (removed.length) fields.removedLabelIds = removed;
  validatePriority(input.priority);
  if (input.priority !== undefined) fields.priority = input.priority;
  const dueDate = validateDate(input.dueDate);
  if (dueDate && input.clearsDueDate) throw new Error('set at most one of dueDate and clearsDueDate');
  if (dueDate) fields.dueDate = dueDate;
  else if (input.clearsDueDate) fields.dueDate = null;
  if (input.title) fields.title = validateText(input.title, 'title');
  if (!Object.keys(fields).length) throw new Error('updateIssue needs at least one change');
  return { locator, variables: { id: locator.id, input: fields } };
}
