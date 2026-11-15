// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { newMutationBranch, newMutationRetry, newMutationUncertain, newQueryBranch, newQueryRetry, type Call, type Failure, type Mutation, type MutationAttempt, type Query, type QueryAttempt, type ReceiptDraft } from '@superdurable/connectors-sdk';
import { CheckpointError, InsufficientTimeError, newFence, type Fence } from '@superdurable/connectors-sdk/singledispatch';
import { CreateIssueDefinition, ListIssuesDefinition } from './generated/definitions.js';
import type { CreateIssueInput, CreatedIssue, Issue, IssuePage, ListIssuesInput } from './generated/models.js';
import { failure, hasAuthMethodScopes, pathEscape, rateLimitDelayMs, requestReceipt, RepositoryAuthMethodID, type GitHubAccess, type GitHubClient, type GitHubExchange } from './client.js';
import { bounded, decode, integerField, invalid, invalidResponse, loginPattern, nextPage, page, providerTime, record, repositoryPattern, safeURL, stringField, timestamp, truncateCharacters } from './operations.js';

const listIssuesOperationId = 'listIssues';
const createIssueOperationId = 'createIssue';
const defaultIssuePageSize = 30;
const defaultIssueBodyCharacters = 4000;
const maximumIssueBodyCharacters = 16384;
const maximumIssueTitleCharacters = 256;
// GitHub accepts issue bodies up to 65536 characters, including the call marker.
const maximumIssueBodyInputCharacters = 65536;
const maximumIssueLabels = 100;
const maximumIssueLabelCharacters = 50;
const maximumIssueAssignees = 10;
const maximumLabels = 100;
const maximumLabelBytes = 256;
const maximumNodeIdBytes = 128;
const maximumIssueStateBytes = 32;
const maximumTitleBytes = 1024;
const maximumAuthorLoginBytes = 100;
// Read-back scans the newest issues the connection's account created, which hold a recent write.
const issueReadBackPageSize = 30;
const issueReadBackPageLimit = 5;
const zeroTime = '0001-01-01T00:00:00Z';
const encoder = new TextEncoder();
const emptyIssuePage: IssuePage = { issues: [], nextPage: 0 };

/** One decoded GitHub issue, validated like the Go connector's JSON model. */
interface GitHubIssue {
  id: number; nodeId: string; number: number; title: string; body: string; state: string; stateReason: string;
  htmlUrl: string; userLogin: string | undefined; labels: string[]; assignees: string[]; comments: number;
  createdAt: string | undefined; updatedAt: string | undefined; closedAt: string | undefined; isPullRequest: boolean;
}

type ReadBackStatus = 'found' | 'notFound' | 'retry' | 'unverifiable';

/** Outcome of reading back a possibly created issue. */
interface IssueReadBack { status: ReadBackStatus; issue?: GitHubIssue; receipt: ReceiptDraft; failure?: Failure; retryAfterMs?: number }

/** Bounded repository issue query, newest created first. */
export class ListIssuesOperation implements Query<ListIssuesInput, IssuePage> {
  definition() { return ListIssuesDefinition; }
  constructor(private readonly client: GitHubClient) {}

  /** Read one page with one GitHub request; the granted scopes must match the connection's method exactly. */
  async invoke(call: Call, input: ListIssuesInput): Promise<QueryAttempt<IssuePage>> {
    const operation = listIssuesOperationId;
    const repository = validateRepository(operation, input.owner, input.repository);
    if (!Array.isArray(repository)) return invalid(operation, emptyIssuePage, repository.message);
    const paging = page(input.pageSize, input.page, defaultIssuePageSize, operation);
    if (!Array.isArray(paging)) return invalid(operation, emptyIssuePage, paging.failure?.message ?? 'invalid page');
    const [size, pageNumber] = paging;
    const query = issueListQuery(input, size, pageNumber);
    if (typeof query === 'string') return invalid(operation, emptyIssuePage, query);
    const maximumBody = input.maxBodyCharacters || defaultIssueBodyCharacters;
    if (!Number.isSafeInteger(maximumBody) || maximumBody < 1 || maximumBody > maximumIssueBodyCharacters) return invalid(operation, emptyIssuePage, 'maxBodyCharacters must be between 1 and 16384');
    const access = await this.client.resolveAccess(call, operation);
    if ('failure' in access) return newQueryBranch('defect', emptyIssuePage, access.receipt, access.failure);
    const exchange = await this.client.get(call, operation, issuesPath(repository[0], repository[1]), query);
    if ('failure' in exchange) {
      if (exchange.branch !== undefined) return newQueryBranch(exchange.branch, emptyIssuePage, exchange.receipt, exchange.failure);
      return newQueryRetry(exchange.failure, exchange.retryAfterMs ?? 0);
    }
    if (!hasAuthMethodScopes(exchange.response.headers, access.authMethodId)) {
      return newQueryBranch('insufficientScope', emptyIssuePage, exchange.receipt, failure(operation, 'AUTHORIZATION', 'GitHub OAuth grant does not match required scopes'));
    }
    let providerIssues: GitHubIssue[];
    try {
      const source = decode(exchange.response.body);
      if (!Array.isArray(source) || source.length > size) throw new TypeError('invalid issue page');
      providerIssues = source.map(decodeGitHubIssue);
    } catch { return invalidResponse(operation, emptyIssuePage, 'GitHub returned an invalid issue page', exchange.receipt); }
    const issues: Issue[] = [];
    for (const providerIssue of providerIssues) {
      if (providerIssue.isPullRequest) continue;
      const issue = issueFromGitHubIssue(providerIssue, maximumBody);
      if (!issue) return invalidResponse(operation, emptyIssuePage, 'GitHub returned an invalid issue', exchange.receipt);
      issues.push(issue);
    }
    let next: number;
    try { next = nextPage(exchange.response.headers, pageNumber); }
    catch { return invalidResponse(operation, emptyIssuePage, 'GitHub returned an invalid pagination link', exchange.receipt); }
    return newQueryBranch('listed', { issues, nextPage: next }, exchange.receipt);
  }
}

/** Fenced issue creation that reads back a possibly accepted send before sending again. */
export class CreateIssueOperation implements Mutation<CreateIssueInput, CreatedIssue> {
  definition() { return CreateIssueDefinition; }
  constructor(private readonly client: GitHubClient) {}

  /** Record the stable Call ID as the key; GitHub accepts none, so the body carries a marker instead. */
  idempotencyKey(callId: string): string { return callId; }

  /**
   * Create one issue behind the shared single-dispatch fence. A possibly accepted earlier send is read back
   * by its saved issue number or by its body marker before another send, and the fence caps the sends of
   * the Call ID at the Step's maxSends.
   */
  async invoke(call: Call, input: CreateIssueInput): Promise<MutationAttempt<CreatedIssue>> {
    const writer = new IssueWriter(this.client, call, issueCallMarker(call.id), requestedIssue(input));
    const request = buildCreateIssueRequest(input, writer);
    if (typeof request === 'string') return newMutationBranch('defect', writer.requested, {}, failure(createIssueOperationId, 'VALIDATION', request));
    let fence: Fence;
    try { fence = newFence(call, CreateIssueDefinition.stepDefaults.executeMethodTimeoutMs ?? 0, { maxSends: CreateIssueDefinition.maxSends ?? 1 }); }
    catch { return writer.branch('defect', 'LOCAL_DEFECT', 'GitHub dispatch fence could not be constructed', {}); }
    const decision = fence.decide();
    if (decision.kind === 'failClosed') return writer.uncertain('PROTOCOL', 'GitHub dispatch checkpoint cannot be trusted', {});
    const access = await this.client.resolveAccess(call, createIssueOperationId);
    let accessFailure = 'failure' in access ? access.failure : undefined;
    if (!accessFailure && 'token' in access && access.authMethodId !== RepositoryAuthMethodID) {
      accessFailure = failure(createIssueOperationId, 'AUTHORIZATION', 'createIssue requires a connection authorized with the github-oauth-repository method');
    }
    if (accessFailure || !('token' in access)) {
      const selected = accessFailure ?? failure(createIssueOperationId, 'AUTHENTICATION', 'GitHub authorization is unavailable or revoked');
      // An earlier attempt may have created the issue, and this connection cannot read it back.
      if (decision.kind !== 'sendAllowed') return newMutationUncertain(writer.requested, selected, {});
      // The branch ends the Step; the marker only lets a repeated Execute send without read-back.
      try { await fence.recordNotSent(); } catch { /* The branch is final without the marker. */ }
      return newMutationBranch(selected.kind === 'AUTHORIZATION' ? 'insufficientScope' : 'defect', writer.requested, {}, selected);
    }
    writer.access = access;
    if (decision.kind === 'readBackRequired' || decision.kind === 'readBackThenResendAllowed') {
      const providerId = decision.marker?.callId === call.id ? decision.marker.providerId ?? '' : '';
      const readBack = await writer.findCreatedIssue(providerId);
      if (readBack.status === 'found') return writer.created(readBack.issue!, readBack.receipt, true);
      if (readBack.status === 'retry') return newMutationRetry(readBack.failure!, readBack.retryAfterMs ?? 0);
      // A saved issue number proves an accepted send, so a missing issue is never sent again.
      if (readBack.status === 'unverifiable' || decision.kind === 'readBackRequired' || providerId) return writer.uncertain('AVAILABILITY', 'GitHub issue creation outcome is unknown', readBack.receipt);
    }
    return writer.dispatch(fence, request, (decision.priorSendUpperBound ?? 0) + 1);
  }
}

/** One createIssue invocation's validated request, access, and call marker. */
class IssueWriter {
  owner = '';
  repository = '';
  access: GitHubAccess | undefined;

  constructor(readonly client: GitHubClient, readonly call: Call, readonly marker: string, readonly requested: CreatedIssue) {}

  /** Send the issue once and classify the answer; claimSendCount counts this send. */
  async dispatch(fence: Fence, request: { title: string; body: string; labels?: string[]; assignees?: string[] }, claimSendCount: number): Promise<MutationAttempt<CreatedIssue>> {
    const payload = encoder.encode(JSON.stringify(request));
    let result;
    try { result = await fence.dispatch(signal => this.client.postFencedWrite(this.access!.token, issuesPath(this.owner, this.repository), payload, signal)); }
    catch (error) {
      if (error instanceof CheckpointError || error instanceof InsufficientTimeError || error instanceof AggregateError) return newMutationRetry(failure(createIssueOperationId, 'AVAILABILITY', 'GitHub dispatch checkpoint or request budget is unavailable'));
      return this.branch('defect', 'LOCAL_DEFECT', 'GitHub dispatch was not allowed', {});
    }
    const isBelowSendCap = claimSendCount < this.maxSends();
    if (!result.response) {
      if (!result.isDispatched) return this.retryUnprocessed(fence, failure(createIssueOperationId, 'TRANSPORT', 'GitHub could not be reached, so no issue was created'), 0);
      return this.reconcileUnknownSend(failure(createIssueOperationId, 'TRANSPORT', 'GitHub issue creation outcome is unknown'), isBelowSendCap, {});
    }
    const response = result.response, receipt = requestReceipt(response.headers), status = response.status;
    if (status >= 200 && status < 300) {
      let created: GitHubIssue | undefined;
      try { if (!result.hasUnusableBody) created = decodeGitHubIssue(decode(response.body)); } catch { created = undefined; }
      if (!created || !isValidCreatedIssue(created)) return this.reconcileAcceptedSend(receipt);
      // The accepted issue number helps after a lost Step result; failing to record it cannot authorize a resend.
      try { await fence.recordProviderId(String(created.number)); } catch { /* See above. */ }
      return this.created(created, receipt, false);
    }
    const delay = rateLimitDelayMs(response, this.client.now());
    if (delay !== undefined) return this.retryUnprocessed(fence, failure(createIssueOperationId, 'RATE_LIMIT', 'GitHub rate limit was reached'), delay);
    if (status === 503) return this.retryUnprocessed(fence, failure(createIssueOperationId, 'AVAILABILITY', 'GitHub is unavailable'), 0);
    if (status === 401) {
      if (await this.client.hasReplacementCredential(this.call)) return this.retryUnprocessed(fence, failure(createIssueOperationId, 'AUTHENTICATION', 'GitHub access token was refreshed for retry'), 0);
      return this.branch('authorizationRevoked', 'AUTHENTICATION', 'GitHub authorization is invalid or revoked', receipt);
    }
    if (status === 403) return this.branch('insufficientScope', 'AUTHORIZATION', 'GitHub authorization is forbidden or lacks scope', receipt);
    if (status === 404) return this.branch('notFound', 'NOT_FOUND', 'GitHub repository was not found', receipt);
    if (status >= 500) return this.reconcileUnknownSend(failure(createIssueOperationId, 'AVAILABILITY', `GitHub issue creation outcome is unknown after HTTP ${status}`), isBelowSendCap, receipt);
    return this.branch('providerRejected', status === 400 || status === 422 ? 'VALIDATION' : 'PROVIDER_REJECTION', `GitHub rejected the issue with HTTP ${status}`, receipt);
  }

  /** Record positive evidence that GitHub did not process the send, then return Retry. */
  async retryUnprocessed(fence: Fence, retryFailure: Failure, delayMs: number): Promise<MutationAttempt<CreatedIssue>> {
    try { await fence.recordCanResend(); }
    catch { return newMutationRetry(failure(createIssueOperationId, 'AVAILABILITY', 'GitHub retry checkpoint could not be recorded'), delayMs); }
    return newMutationRetry(retryFailure, delayMs);
  }

  /** Read back an accepted issue whose answer was unusable; it never sends again. */
  async reconcileAcceptedSend(sendReceipt: ReceiptDraft): Promise<MutationAttempt<CreatedIssue>> {
    const readBack = await this.findCreatedIssue('');
    if (readBack.status === 'found') return this.created(readBack.issue!, readBack.receipt, true);
    return this.uncertain('PROTOCOL', 'GitHub accepted the issue but returned an unusable issue', sendReceipt);
  }

  /** Read back a possible send; below the cap a miss retries, at the cap it is uncertain. */
  async reconcileUnknownSend(sendFailure: Failure, isBelowSendCap: boolean, sendReceipt: ReceiptDraft): Promise<MutationAttempt<CreatedIssue>> {
    const readBack = await this.findCreatedIssue('');
    if (readBack.status === 'found') return this.created(readBack.issue!, readBack.receipt, true);
    if (readBack.status === 'retry' || isBelowSendCap) return newMutationRetry(sendFailure, readBack.retryAfterMs ?? 0);
    return newMutationUncertain(this.requested, sendFailure, sendReceipt);
  }

  /** Read the saved issue number, or scan the account's newest issues for the marker. */
  async findCreatedIssue(providerId: string): Promise<IssueReadBack> {
    if (providerId) {
      const number = /^\d+$/.test(providerId) ? Number(providerId) : 0;
      if (!Number.isSafeInteger(number) || number <= 0) return { status: 'unverifiable', receipt: {} };
      const readBack = await this.readIssueResponse(`${issuesPath(this.owner, this.repository)}/${number}`);
      if (readBack.status !== 'found' || !readBack.exchange) return readBack;
      try {
        // The number comes from this Call ID's accepted send, so it needs no body marker.
        const candidate = decodeGitHubIssue(decode(readBack.exchange.response.body));
        if (candidate.number === number && isValidCreatedIssue(candidate)) return { status: 'found', issue: candidate, receipt: readBack.receipt };
      } catch { /* An undecodable issue cannot confirm the send. */ }
      return { status: 'unverifiable', receipt: readBack.receipt };
    }
    let readBack = await this.readIssueResponse('/user');
    if (readBack.status !== 'found' || !readBack.exchange) return readBack;
    let login: string;
    try { login = stringField(record(decode(readBack.exchange.response.body)), 'login'); } catch { return { status: 'unverifiable', receipt: readBack.receipt }; }
    if (!loginPattern.test(login)) return { status: 'unverifiable', receipt: readBack.receipt };
    for (let pageNumber = 1; pageNumber <= issueReadBackPageLimit; pageNumber++) {
      readBack = await this.readIssueResponse(issuesPath(this.owner, this.repository), {
        creator: login, state: 'all', sort: 'created', direction: 'desc', per_page: String(issueReadBackPageSize), page: String(pageNumber),
      });
      if (readBack.status !== 'found' || !readBack.exchange) return readBack;
      let candidates: GitHubIssue[];
      try {
        const source = decode(readBack.exchange.response.body);
        if (!Array.isArray(source)) throw new TypeError('invalid issue page');
        candidates = source.map(decodeGitHubIssue);
      } catch { return { status: 'unverifiable', receipt: readBack.receipt }; }
      const match = candidates.find(candidate => !candidate.isPullRequest && candidate.userLogin !== undefined &&
        candidate.userLogin.toLowerCase() === login.toLowerCase() && isValidCreatedIssue(candidate) && candidate.body.includes(this.marker));
      if (match) return { status: 'found', issue: match, receipt: readBack.receipt };
      let next: number;
      try { next = nextPage(readBack.exchange.response.headers, pageNumber); } catch { return { status: 'unverifiable', receipt: readBack.receipt }; }
      if (next === 0) break;
    }
    return { status: 'notFound', receipt: readBack.receipt };
  }

  /** Perform one read-back GET; transient failures retry and other refusals cannot verify. */
  async readIssueResponse(path: string, query: Record<string, string> = {}): Promise<IssueReadBack & { exchange?: Extract<GitHubExchange, { response: unknown }> }> {
    const exchange = await this.client.get(this.call, createIssueOperationId, path, query);
    if (!('failure' in exchange)) return { status: 'found', receipt: exchange.receipt, exchange };
    if (exchange.isTransportFailure) return { status: 'retry', receipt: {}, failure: failure(createIssueOperationId, 'TRANSPORT', 'GitHub issue read-back is temporarily unavailable'), retryAfterMs: 0 };
    if (exchange.branch === undefined) return { status: 'retry', receipt: exchange.receipt, failure: exchange.failure, retryAfterMs: exchange.retryAfterMs ?? 0 };
    return { status: 'unverifiable', receipt: exchange.receipt };
  }

  created(issue: GitHubIssue, issueReceipt: ReceiptDraft, isFoundByReadBack: boolean): MutationAttempt<CreatedIssue> {
    const output: CreatedIssue = { ...this.requested, title: bounded(issue.title, maximumTitleBytes), number: issue.number };
    if (issue.id > 0) output.id = issue.id;
    output.nodeId = issue.nodeId;
    const url = safeURL(issue.htmlUrl); if (url) output.url = url;
    output.state = bounded(issue.state, maximumIssueStateBytes);
    output.createdAt = issue.createdAt!;
    const labels = boundedValues(issue.labels, maximumLabels, maximumLabelBytes), assignees = boundedValues(issue.assignees, maximumLabels, maximumAuthorLoginBytes);
    if (labels.length) output.labels = labels; if (assignees.length) output.assignees = assignees;
    const receipt: ReceiptDraft = { ...issueReceipt, providerObjectId: issue.nodeId, ...(isFoundByReadBack ? { metadata: { foundByReadBack: 'true' } } : {}) };
    return newMutationBranch('created', output, receipt);
  }

  branch(branch: string, kind: Failure['kind'], message: string, receipt: ReceiptDraft): MutationAttempt<CreatedIssue> {
    return newMutationBranch(branch, this.requested, receipt, failure(createIssueOperationId, kind, message));
  }

  uncertain(kind: Failure['kind'], message: string, receipt: ReceiptDraft): MutationAttempt<CreatedIssue> {
    return newMutationUncertain(this.requested, failure(createIssueOperationId, kind, message), receipt);
  }

  /** The send cap the Fence applies: the Step's override for a limited operation, else the manifest cap. */
  maxSends(): number {
    const definitionMaxSends = CreateIssueDefinition.maxSends ?? 1;
    return definitionMaxSends >= 2 ? this.call.maxSendsOverride ?? definitionMaxSends : 1;
  }
}

/** Return the hidden Markdown comment holding a digest of the Call ID, never Flow input. */
export function issueCallMarker(callId: string): string {
  return `<!-- dex-connector-call: ${createHash('sha256').update(callId).digest('hex')} -->`;
}

function requestedIssue(input: CreateIssueInput): CreatedIssue {
  return { owner: String(input.owner ?? '').trim(), repository: String(input.repository ?? '').trim(), title: String(input.title ?? '').trim() };
}

/** Validate input and append the call marker to the body; a string is the validation failure. */
function buildCreateIssueRequest(input: CreateIssueInput, writer: IssueWriter): { title: string; body: string; labels?: string[]; assignees?: string[] } | string {
  const repository = validateRepository(createIssueOperationId, input.owner, input.repository);
  if (!Array.isArray(repository)) return repository.message;
  [writer.owner, writer.repository] = repository;
  const title = writer.requested.title;
  if (!title) return 'GitHub issue title is required';
  if (!isSingleLineText(title) || Array.from(title).length > maximumIssueTitleCharacters) return 'GitHub issue title must be one line of at most 256 characters';
  const inputBody = input.body ?? '', markerSuffix = `\n\n${writer.marker}`;
  if (!isWellFormedText(inputBody) || inputBody.includes('\0') || Array.from(inputBody).length + Array.from(markerSuffix).length > maximumIssueBodyInputCharacters) {
    return 'GitHub issue body must be valid text of at most 65536 characters including the connector marker';
  }
  const body = inputBody.trim() ? inputBody + markerSuffix : writer.marker;
  const inputLabels = input.labels ?? [], inputAssignees = input.assignees ?? [];
  if (inputLabels.length > maximumIssueLabels) return 'GitHub issue has more than 100 labels';
  const labels: string[] = [];
  for (const value of inputLabels) {
    const label = value.trim();
    if (!label || !isSingleLineText(label) || Array.from(label).length > maximumIssueLabelCharacters) return 'GitHub issue labels must be one line of 1 through 50 characters';
    labels.push(label);
  }
  if (inputAssignees.length > maximumIssueAssignees) return 'GitHub issue has more than 10 assignees';
  const assignees: string[] = [];
  for (const value of inputAssignees) {
    const assignee = value.trim();
    if (!loginPattern.test(assignee)) return 'GitHub issue assignee login is invalid';
    assignees.push(assignee);
  }
  return { title, body, ...(labels.length ? { labels } : {}), ...(assignees.length ? { assignees } : {}) };
}

/** Validate the listIssues filters and return GitHub's query, newest created first; a string is the failure. */
function issueListQuery(input: ListIssuesInput, size: number, pageNumber: number): Record<string, string> | string {
  let state = (input.state ?? '').trim();
  if (state === '') state = 'open';
  else if (!['open', 'closed', 'all'].includes(state)) return 'issue state must be open, closed, or all';
  const query: Record<string, string> = { state, sort: 'created', direction: 'desc', per_page: String(size), page: String(pageNumber) };
  const inputLabels = input.labels ?? [];
  if (inputLabels.length > maximumLabels) return 'issue label filter has more than 100 labels';
  const labels: string[] = [];
  for (const value of inputLabels) {
    const label = value.trim();
    if (!label || encoder.encode(label).length > maximumLabelBytes || !isSingleLineText(label) || label.includes(',')) return 'issue label filter names must be one line without commas';
    labels.push(label);
  }
  if (labels.length) query.labels = labels.join(',');
  const creator = (input.creator ?? '').trim();
  if (creator) {
    if (!loginPattern.test(creator)) return 'issue creator login is invalid';
    query.creator = creator;
  }
  if (input.updatedSince != null) {
    let since: string | undefined;
    try { since = timestamp(input.updatedSince, true); } catch { since = undefined; }
    if (!since || since === zeroTime) return 'issue updatedSince must be a time';
    query.since = providerTime(since);
  }
  return query;
}

function validateRepository(operation: string, owner: string, repository: string): [string, string] | Failure {
  const trimmedOwner = String(owner ?? '').trim(), trimmedRepository = String(repository ?? '').trim();
  if (!loginPattern.test(trimmedOwner)) return failure(operation, 'VALIDATION', 'GitHub repository owner is invalid');
  if (!repositoryPattern.test(trimmedRepository) || trimmedRepository === '.' || trimmedRepository === '..') return failure(operation, 'VALIDATION', 'GitHub repository name is invalid');
  return [trimmedOwner, trimmedRepository];
}

/** Decode one issue with the field types the Go connector's JSON model accepts. */
function decodeGitHubIssue(value: unknown): GitHubIssue {
  const source = record(value);
  const user = source.user == null ? undefined : record(source.user);
  const names = (items: unknown, key: string): string[] => {
    if (items == null) return [];
    if (!Array.isArray(items)) throw new TypeError(`invalid GitHub ${key}`);
    return items.map(item => stringField(record(item), key));
  };
  if (source.pull_request != null) record(source.pull_request);
  return {
    id: integerField(source, 'id'), nodeId: stringField(source, 'node_id'), number: integerField(source, 'number'),
    title: stringField(source, 'title'), body: stringField(source, 'body'), state: stringField(source, 'state'),
    stateReason: stringField(source, 'state_reason'), htmlUrl: stringField(source, 'html_url'),
    userLogin: user === undefined ? undefined : stringField(user, 'login'),
    labels: names(source.labels, 'name'), assignees: names(source.assignees, 'login'), comments: integerField(source, 'comments'),
    createdAt: timestamp(source.created_at), updatedAt: timestamp(source.updated_at), closedAt: timestamp(source.closed_at),
    isPullRequest: source.pull_request != null,
  };
}

function issueFromGitHubIssue(providerIssue: GitHubIssue, maximumBody: number): Issue | undefined {
  const state = bounded(providerIssue.state, maximumIssueStateBytes);
  if (providerIssue.number <= 0 || !state || !providerIssue.createdAt || !providerIssue.updatedAt || encoder.encode(providerIssue.nodeId).length > maximumNodeIdBytes) return undefined;
  const [body, bodyTruncated] = truncateCharacters(providerIssue.body, maximumBody);
  const issue: Issue = {
    number: providerIssue.number, nodeId: providerIssue.nodeId, title: bounded(providerIssue.title, maximumTitleBytes), bodyTruncated, state,
    comments: Math.max(providerIssue.comments, 0), createdAt: providerIssue.createdAt, updatedAt: providerIssue.updatedAt,
  };
  if (body) issue.body = body;
  const stateReason = bounded(providerIssue.stateReason, maximumIssueStateBytes); if (stateReason) issue.stateReason = stateReason;
  const url = safeURL(providerIssue.htmlUrl); if (url) issue.url = url;
  const authorLogin = providerIssue.userLogin === undefined ? '' : bounded(providerIssue.userLogin, maximumAuthorLoginBytes); if (authorLogin) issue.authorLogin = authorLogin;
  const labels = boundedValues(providerIssue.labels, maximumLabels, maximumLabelBytes); if (labels.length) issue.labels = labels;
  const assignees = boundedValues(providerIssue.assignees, maximumLabels, maximumAuthorLoginBytes); if (assignees.length) issue.assignees = assignees;
  if (providerIssue.closedAt) issue.closedAt = providerIssue.closedAt;
  return issue;
}

function isValidCreatedIssue(issue: GitHubIssue): boolean {
  return issue.number > 0 && issue.nodeId !== '' && encoder.encode(issue.nodeId).length <= maximumNodeIdBytes &&
    issue.state.trim() !== '' && issue.createdAt !== undefined && issue.createdAt !== zeroTime;
}

function boundedValues(values: string[], maximumValues: number, maximumBytes: number): string[] {
  return values.slice(0, maximumValues).map(value => bounded(value, maximumBytes)).filter(Boolean);
}

function issuesPath(owner: string, repository: string): string { return `/repos/${pathEscape(owner)}/${pathEscape(repository)}/issues`; }

function isSingleLineText(value: string): boolean { return isWellFormedText(value) && !/\p{Cc}/u.test(value); }

// In Unicode mode a surrogate class matches only an unpaired surrogate, which is not valid UTF-8.
function isWellFormedText(value: string): boolean { return !/\p{Cs}/u.test(value); }
