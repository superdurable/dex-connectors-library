// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { newMutationBranch, newMutationRetry, newMutationUncertain, newQueryBranch, newQueryRetry, type Call, type Failure, type Mutation, type MutationAttempt, type Query, type QueryAttempt, type ReceiptDraft } from '@superdurable/connectors-sdk';
import { CheckpointError, InsufficientTimeError, newFence, type Fence } from '@superdurable/connectors-sdk/singledispatch';
import { CreateIssueCommentDefinition, CreateIssueDefinition, ListIssuesDefinition, ReplaceIssueLabelsDefinition, SetIssueStateDefinition } from './generated/definitions.js';
import type { CreateIssueCommentInput, CreateIssueInput, CreatedIssue, CreatedIssueComment, Issue, IssuePage, ListIssuesInput, ReplaceIssueLabelsInput, ReplacedIssueLabels, SetIssueStateInput, UpdatedIssueState } from './generated/models.js';
import { failure, hasAuthMethodScopes, pathEscape, rateLimitDelayMs, requestReceipt, RepositoryAuthMethodID, type GitHubAccess, type GitHubClient, type GitHubExchange } from './client.js';
import { bounded, decode, integerField, invalid, invalidResponse, loginPattern, nextPage, page, providerTime, record, repositoryPattern, safeURL, stringField, timestamp, truncateCharacters } from './operations.js';

const listIssuesOperationId = 'listIssues';
const createIssueOperationId = 'createIssue';
const setIssueStateOperationId = 'setIssueState';
const replaceIssueLabelsOperationId = 'replaceIssueLabels';
const createIssueCommentOperationId = 'createIssueComment';
// Read-back scans the first page and the newest comment pages, which hold a recent write.
const commentReadBackPageSize = 100;
const commentReadBackPageLimit = 5;
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

/** An open issue. */
export const IssueStateOpen = 'open';
/** A closed issue. */
export const IssueStateClosed = 'closed';
/** Closes an issue as completed; GitHub applies it when a close omits the reason. */
export const IssueStateReasonCompleted = 'completed';
/** Closes an issue as not planned. */
export const IssueStateReasonNotPlanned = 'not_planned';
/** Reopens a closed issue; GitHub applies it when a reopen omits the reason. */
export const IssueStateReasonReopened = 'reopened';

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

/** Issue close and reopen Mutation; an absolute state is safe to send again after an unknown outcome. */
export class SetIssueStateOperation implements Mutation<SetIssueStateInput, UpdatedIssueState> {
  definition() { return SetIssueStateDefinition; }
  constructor(private readonly client: GitHubClient) {}

  /** Record the stable Call ID as the key; GitHub accepts none, and the update is safe to repeat. */
  idempotencyKey(callId: string): string { return callId; }

  /**
   * Send one PATCH that sets the issue's state. Invalid input and a connection without the
   * github-oauth-repository method select a branch before GitHub is called; rate limits, 5xx answers, and
   * transport failures retry, because sending the same absolute state again is harmless.
   */
  async invoke(call: Call, input: SetIssueStateInput): Promise<MutationAttempt<UpdatedIssueState>> {
    const operation = setIssueStateOperationId;
    const requested: UpdatedIssueState = { number: Number.isSafeInteger(input.issueNumber) ? Math.max(input.issueNumber, 0) : 0 };
    const request = buildSetIssueStateRequest(input);
    if (!('body' in request)) return newMutationBranch('defect', requested, {}, request);
    const access = await this.client.resolveAccess(call, operation);
    if ('failure' in access) return newMutationBranch('defect', requested, access.receipt, access.failure);
    if (access.authMethodId !== RepositoryAuthMethodID) {
      return newMutationBranch('insufficientScope', requested, {}, failure(operation, 'AUTHORIZATION', 'setIssueState requires a connection authorized with the github-oauth-repository method'));
    }
    const payload = encoder.encode(JSON.stringify(request.body));
    const exchange = await this.client.sendRequest(call, operation, 'PATCH', `${issuesPath(request.owner, request.repository)}/${input.issueNumber}`, {}, payload);
    if ('failure' in exchange) {
      if (exchange.branch === undefined) return newMutationRetry(exchange.failure, exchange.retryAfterMs ?? 0);
      return newMutationBranch(exchange.branch, requested, exchange.receipt, exchange.failure);
    }
    let updated: GitHubIssue;
    try { updated = decodeGitHubIssue(decode(exchange.response.body)); }
    catch { return newMutationBranch('invalidResponse', requested, exchange.receipt, failure(operation, 'PROTOCOL', 'GitHub returned an invalid issue')); }
    if (!isValidUpdatedIssue(updated, input.issueNumber, request.body.state)) {
      return newMutationBranch('invalidResponse', requested, exchange.receipt, failure(operation, 'PROTOCOL', 'GitHub returned an invalid issue'));
    }
    const output: UpdatedIssueState = { number: updated.number };
    const url = safeURL(updated.htmlUrl); if (url) output.url = url;
    output.state = updated.state;
    const stateReason = bounded(updated.stateReason, maximumIssueStateBytes); if (stateReason) output.stateReason = stateReason;
    output.updatedAt = updated.updatedAt!;
    return newMutationBranch('updated', output, { ...exchange.receipt, ...(updated.nodeId ? { providerObjectId: updated.nodeId } : {}) });
  }
}

/** Validate the state and reason against GitHub's combinations; a Failure selects defect. */
function buildSetIssueStateRequest(input: SetIssueStateInput): { owner: string; repository: string; body: { state: string; state_reason?: string } } | Failure {
  const operation = setIssueStateOperationId;
  const repository = validateRepository(operation, input.owner, input.repository);
  if (!Array.isArray(repository)) return repository;
  if (!Number.isSafeInteger(input.issueNumber) || input.issueNumber <= 0) return failure(operation, 'VALIDATION', 'GitHub issue number must be positive');
  const state = String(input.state ?? '').trim(), stateReason = String(input.stateReason ?? '').trim();
  if (state === IssueStateOpen) {
    if (stateReason && stateReason !== IssueStateReasonReopened) return failure(operation, 'VALIDATION', 'GitHub issue state open accepts only the state reason reopened');
  } else if (state === IssueStateClosed) {
    if (stateReason && stateReason !== IssueStateReasonCompleted && stateReason !== IssueStateReasonNotPlanned) return failure(operation, 'VALIDATION', 'GitHub issue state closed accepts only the state reasons completed and not_planned');
  } else return failure(operation, 'VALIDATION', 'GitHub issue state must be open or closed');
  return { owner: repository[0], repository: repository[1], body: { state, ...(stateReason ? { state_reason: stateReason } : {}) } };
}

// Requiring the requested issue in the requested state rules out a redirect that read a transferred issue.
function isValidUpdatedIssue(issue: GitHubIssue, issueNumber: number, state: string): boolean {
  return issue.number === issueNumber && issue.state === state && issue.updatedAt !== undefined && issue.updatedAt !== zeroTime &&
    encoder.encode(issue.nodeId).length <= maximumNodeIdBytes;
}

/** Issue label replacement; the full label set is safe to send again after an unknown outcome. */
export class ReplaceIssueLabelsOperation implements Mutation<ReplaceIssueLabelsInput, ReplacedIssueLabels> {
  definition() { return ReplaceIssueLabelsDefinition; }
  constructor(private readonly client: GitHubClient) {}

  /** Record the stable Call ID as the key; GitHub accepts none, and the replacement is safe to repeat. */
  idempotencyKey(callId: string): string { return callId; }

  /**
   * Send one PUT that replaces the issue's labels. Invalid input and a connection without the
   * github-oauth-repository method select a branch before GitHub is called; rate limits, 5xx answers, and
   * transport failures retry, because sending the same full set again is harmless.
   */
  async invoke(call: Call, input: ReplaceIssueLabelsInput): Promise<MutationAttempt<ReplacedIssueLabels>> {
    const operation = replaceIssueLabelsOperationId;
    const requested: ReplacedIssueLabels = { number: Number.isSafeInteger(input.issueNumber) ? Math.max(input.issueNumber, 0) : 0, labels: [] };
    const request = buildReplaceIssueLabelsRequest(input);
    if (!('body' in request)) return newMutationBranch('defect', requested, {}, request);
    const access = await this.client.resolveAccess(call, operation);
    if ('failure' in access) return newMutationBranch('defect', requested, access.receipt, access.failure);
    if (access.authMethodId !== RepositoryAuthMethodID) {
      return newMutationBranch('insufficientScope', requested, {}, failure(operation, 'AUTHORIZATION', 'replaceIssueLabels requires a connection authorized with the github-oauth-repository method'));
    }
    const payload = encoder.encode(JSON.stringify(request.body));
    const exchange = await this.client.sendRequest(call, operation, 'PUT', `${issuesPath(request.owner, request.repository)}/${input.issueNumber}/labels`, {}, payload);
    if ('failure' in exchange) {
      if (exchange.branch === undefined) return newMutationRetry(exchange.failure, exchange.retryAfterMs ?? 0);
      return newMutationBranch(exchange.branch, requested, exchange.receipt, exchange.failure);
    }
    let names: string[];
    try {
      const source = decode(exchange.response.body);
      if (!Array.isArray(source) || source.length > maximumIssueLabels) throw new TypeError('invalid label list');
      names = source.map(label => stringField(record(label), 'name'));
    } catch { return newMutationBranch('invalidResponse', requested, exchange.receipt, failure(operation, 'PROTOCOL', 'GitHub returned an invalid label list')); }
    const output: ReplacedIssueLabels = { number: input.issueNumber, labels: boundedValues(names, maximumLabels, maximumLabelBytes) };
    const url = issueWebUrl(this.client.baseUrl, request.owner, request.repository, input.issueNumber); if (url) output.url = url;
    return newMutationBranch('replaced', output, exchange.receipt);
  }
}

/** Validate the repository, issue number, and label names; a Failure selects defect. */
function buildReplaceIssueLabelsRequest(input: ReplaceIssueLabelsInput): { owner: string; repository: string; body: { labels: string[] } } | Failure {
  const operation = replaceIssueLabelsOperationId;
  const repository = validateRepository(operation, input.owner, input.repository);
  if (!Array.isArray(repository)) return repository;
  if (!Number.isSafeInteger(input.issueNumber) || input.issueNumber <= 0) return failure(operation, 'VALIDATION', 'GitHub issue number must be positive');
  const sourceLabels = input.labels ?? [];
  if (sourceLabels.length > maximumIssueLabels) return failure(operation, 'VALIDATION', 'GitHub issue has more than 100 labels');
  const labels: string[] = [];
  for (const sourceLabel of sourceLabels) {
    const label = String(sourceLabel ?? '').trim();
    if (!label || !isSingleLineText(label) || [...label].length > maximumIssueLabelCharacters) return failure(operation, 'VALIDATION', 'GitHub issue labels must be one line of 1 through 50 characters');
    labels.push(label);
  }
  return { owner: repository[0], repository: repository[1], body: { labels } };
}

/** Map the API base URL to the web host: api.github.com to github.com, and a GitHub Enterprise Server /api/v3 URL to its host. */
function issueWebUrl(baseUrl: string, owner: string, repository: string, issueNumber: number): string {
  const base = new URL(baseUrl);
  const path = base.pathname.replace(/\/+$/, '');
  let host: string;
  if (base.hostname.toLowerCase() === 'api.github.com' && path === '') host = 'github.com';
  else if (path === '/api/v3') host = base.host;
  else return '';
  return `${base.protocol}//${host}/${owner}/${repository}/issues/${issueNumber}`;
}

/** One decoded GitHub issue comment, validated like the Go connector's JSON model. */
interface GitHubIssueComment { id: number; body: string; htmlUrl: string; userLogin: string | undefined; createdAt: string | undefined }

/** Outcome of reading back a possibly created comment. */
interface CommentReadBack { status: ReadBackStatus; comment?: GitHubIssueComment; receipt: ReceiptDraft; failure?: Failure; retryAfterMs?: number }

/** Fenced issue comment creation that reads back a possibly accepted send before sending again. */
export class CreateIssueCommentOperation implements Mutation<CreateIssueCommentInput, CreatedIssueComment> {
  definition() { return CreateIssueCommentDefinition; }
  constructor(private readonly client: GitHubClient) {}

  /** Record the stable Call ID as the key; GitHub accepts none, so the body carries a marker instead. */
  idempotencyKey(callId: string): string { return callId; }

  /**
   * Create one comment behind the shared single-dispatch fence. A possibly accepted earlier send is read
   * back by its saved comment ID or by its body marker before another send, and the fence caps the sends of
   * the Call ID at the Step's maxSends.
   */
  async invoke(call: Call, input: CreateIssueCommentInput): Promise<MutationAttempt<CreatedIssueComment>> {
    const writer = new CommentWriter(this.client, call, issueCallMarker(call.id), { issueNumber: Number.isSafeInteger(input.issueNumber) ? Math.max(input.issueNumber, 0) : 0 });
    const request = buildCreateIssueCommentRequest(input, writer);
    if (typeof request === 'string') return newMutationBranch('defect', writer.requested, {}, failure(createIssueCommentOperationId, 'VALIDATION', request));
    let fence: Fence;
    try { fence = newFence(call, CreateIssueCommentDefinition.stepDefaults.executeMethodTimeoutMs ?? 0, { maxSends: writer.maxSends() }); }
    catch { return writer.branch('defect', 'LOCAL_DEFECT', 'GitHub dispatch fence could not be constructed', {}); }
    const decision = fence.decide();
    if (decision.kind === 'failClosed') return writer.uncertain('PROTOCOL', 'GitHub dispatch checkpoint cannot be trusted', {});
    const access = await this.client.resolveAccess(call, createIssueCommentOperationId);
    let accessFailure = 'failure' in access ? access.failure : undefined;
    if (!accessFailure && 'token' in access && access.authMethodId !== RepositoryAuthMethodID) {
      accessFailure = failure(createIssueCommentOperationId, 'AUTHORIZATION', 'createIssueComment requires a connection authorized with the github-oauth-repository method');
    }
    if (accessFailure || !('token' in access)) {
      const selected = accessFailure ?? failure(createIssueCommentOperationId, 'AUTHENTICATION', 'GitHub authorization is unavailable or revoked');
      // An earlier attempt may have created the comment, and this connection cannot read it back.
      if (decision.kind !== 'sendAllowed') return newMutationUncertain(writer.requested, selected, {});
      // The branch ends the Step; the marker only lets a repeated Execute send without read-back.
      try { await fence.recordNotSent(); } catch { /* The branch is final without the marker. */ }
      return newMutationBranch(selected.kind === 'AUTHORIZATION' ? 'insufficientScope' : 'defect', writer.requested, {}, selected);
    }
    writer.access = access;
    if (decision.kind === 'readBackRequired' || decision.kind === 'readBackThenResendAllowed') {
      const providerId = decision.marker?.callId === call.id ? decision.marker.providerId ?? '' : '';
      const readBack = await writer.findCreatedComment(providerId);
      if (readBack.status === 'found') return writer.created(readBack.comment!, readBack.receipt, true);
      if (readBack.status === 'retry') return newMutationRetry(readBack.failure!, readBack.retryAfterMs ?? 0);
      // A saved comment ID proves an accepted send, so a missing comment is never sent again.
      if (readBack.status === 'unverifiable' || decision.kind === 'readBackRequired' || providerId) return writer.uncertain('AVAILABILITY', 'GitHub comment creation outcome is unknown', readBack.receipt);
    }
    return writer.dispatch(fence, request, (decision.priorSendUpperBound ?? 0) + 1);
  }
}

/** One createIssueComment invocation's validated request, access, and call marker. */
class CommentWriter {
  owner = '';
  repository = '';
  access: GitHubAccess | undefined;

  constructor(readonly client: GitHubClient, readonly call: Call, readonly marker: string, readonly requested: CreatedIssueComment) {}

  /** Send the comment once and classify the answer; claimSendCount counts this send. */
  async dispatch(fence: Fence, request: { body: string }, claimSendCount: number): Promise<MutationAttempt<CreatedIssueComment>> {
    const payload = encoder.encode(JSON.stringify(request));
    let result;
    try { result = await fence.dispatch(signal => this.client.postFencedWrite(this.access!.token, this.commentsPath(), payload, signal)); }
    catch (error) {
      if (error instanceof CheckpointError || error instanceof InsufficientTimeError || error instanceof AggregateError) return newMutationRetry(failure(createIssueCommentOperationId, 'AVAILABILITY', 'GitHub dispatch checkpoint or request budget is unavailable'));
      return this.branch('defect', 'LOCAL_DEFECT', 'GitHub dispatch was not allowed', {});
    }
    const isBelowSendCap = claimSendCount < this.maxSends();
    if (!result.response) {
      if (!result.isDispatched) return this.retryUnprocessed(fence, failure(createIssueCommentOperationId, 'TRANSPORT', 'GitHub could not be reached, so no comment was created'), 0);
      return this.reconcileUnknownSend(failure(createIssueCommentOperationId, 'TRANSPORT', 'GitHub comment creation outcome is unknown'), isBelowSendCap, {});
    }
    const response = result.response, receipt = requestReceipt(response.headers), status = response.status;
    if (status >= 200 && status < 300) {
      let created: GitHubIssueComment | undefined;
      try { if (!result.hasUnusableBody) created = decodeGitHubIssueComment(decode(response.body)); } catch { created = undefined; }
      if (!created || !isValidCreatedComment(created)) return this.reconcileAcceptedSend(receipt);
      // The accepted comment ID helps after a lost Step result; failing to record it cannot authorize a resend.
      try { await fence.recordProviderId(String(created.id)); } catch { /* See above. */ }
      return this.created(created, receipt, false);
    }
    const delay = rateLimitDelayMs(response, this.client.now());
    if (delay !== undefined) return this.retryUnprocessed(fence, failure(createIssueCommentOperationId, 'RATE_LIMIT', 'GitHub rate limit was reached'), delay);
    if (status === 503) return this.retryUnprocessed(fence, failure(createIssueCommentOperationId, 'AVAILABILITY', 'GitHub is unavailable'), 0);
    if (status === 401) {
      if (await this.client.hasReplacementCredential(this.call)) return this.retryUnprocessed(fence, failure(createIssueCommentOperationId, 'AUTHENTICATION', 'GitHub access token was refreshed for retry'), 0);
      return this.branch('authorizationRevoked', 'AUTHENTICATION', 'GitHub authorization is invalid or revoked', receipt);
    }
    if (status === 403) return this.branch('insufficientScope', 'AUTHORIZATION', 'GitHub authorization is forbidden or lacks scope', receipt);
    if (status === 404) return this.branch('notFound', 'NOT_FOUND', 'GitHub repository or issue was not found', receipt);
    if (status >= 500) return this.reconcileUnknownSend(failure(createIssueCommentOperationId, 'AVAILABILITY', `GitHub comment creation outcome is unknown after HTTP ${status}`), isBelowSendCap, receipt);
    return this.branch('providerRejected', status === 400 || status === 422 ? 'VALIDATION' : 'PROVIDER_REJECTION', `GitHub rejected the comment with HTTP ${status}`, receipt);
  }

  /** Record positive evidence that GitHub did not process the send, then return Retry. */
  async retryUnprocessed(fence: Fence, retryFailure: Failure, delayMs: number): Promise<MutationAttempt<CreatedIssueComment>> {
    try { await fence.recordCanResend(); }
    catch { return newMutationRetry(failure(createIssueCommentOperationId, 'AVAILABILITY', 'GitHub retry checkpoint could not be recorded'), delayMs); }
    return newMutationRetry(retryFailure, delayMs);
  }

  /** Read back an accepted comment whose answer was unusable; it never sends again. */
  async reconcileAcceptedSend(sendReceipt: ReceiptDraft): Promise<MutationAttempt<CreatedIssueComment>> {
    const readBack = await this.findCreatedComment('');
    if (readBack.status === 'found') return this.created(readBack.comment!, readBack.receipt, true);
    return this.uncertain('PROTOCOL', 'GitHub accepted the comment but returned an unusable comment', sendReceipt);
  }

  /** Read back a possible send; below the cap a miss retries, at the cap it is uncertain. */
  async reconcileUnknownSend(sendFailure: Failure, isBelowSendCap: boolean, sendReceipt: ReceiptDraft): Promise<MutationAttempt<CreatedIssueComment>> {
    const readBack = await this.findCreatedComment('');
    if (readBack.status === 'found') return this.created(readBack.comment!, readBack.receipt, true);
    if (readBack.status === 'retry' || isBelowSendCap) return newMutationRetry(sendFailure, readBack.retryAfterMs ?? 0);
    return newMutationUncertain(this.requested, sendFailure, sendReceipt);
  }

  /** Read the saved comment ID, or scan the first and newest comment pages for the account's marked comment. */
  async findCreatedComment(providerId: string): Promise<CommentReadBack> {
    if (providerId) {
      const commentId = /^\d+$/.test(providerId) ? Number(providerId) : 0;
      if (!Number.isSafeInteger(commentId) || commentId <= 0) return { status: 'unverifiable', receipt: {} };
      const readBack = await this.readCommentResponse(`${issuesPath(this.owner, this.repository)}/comments/${commentId}`);
      if (readBack.status !== 'found' || !readBack.exchange) return readBack;
      try {
        // The ID comes from this Call ID's accepted send, so it needs no body marker.
        const candidate = decodeGitHubIssueComment(decode(readBack.exchange.response.body));
        if (candidate.id === commentId && isValidCreatedComment(candidate)) return { status: 'found', comment: candidate, receipt: readBack.receipt };
      } catch { /* An undecodable comment cannot confirm the send. */ }
      return { status: 'unverifiable', receipt: readBack.receipt };
    }
    let readBack = await this.readCommentResponse('/user');
    if (readBack.status !== 'found' || !readBack.exchange) return readBack;
    let login: string;
    try { login = stringField(record(decode(readBack.exchange.response.body)), 'login'); } catch { return { status: 'unverifiable', receipt: readBack.receipt }; }
    if (!loginPattern.test(login)) return { status: 'unverifiable', receipt: readBack.receipt };
    // GitHub lists comments oldest first, so the newest comments sit on the last page.
    for (let pageNumber = 1, pagesRead = 0; pageNumber > 0 && pagesRead < commentReadBackPageLimit; pagesRead++) {
      readBack = await this.readCommentResponse(this.commentsPath(), { per_page: String(commentReadBackPageSize), page: String(pageNumber) });
      if (readBack.status !== 'found' || !readBack.exchange) return readBack;
      let candidates: GitHubIssueComment[];
      try {
        const source = decode(readBack.exchange.response.body);
        if (!Array.isArray(source)) throw new TypeError('invalid comment page');
        candidates = source.map(decodeGitHubIssueComment);
      } catch { return { status: 'unverifiable', receipt: readBack.receipt }; }
      const match = candidates.find(candidate => candidate.userLogin !== undefined && candidate.userLogin.toLowerCase() === login.toLowerCase() &&
        isValidCreatedComment(candidate) && candidate.body.includes(this.marker));
      if (match) return { status: 'found', comment: match, receipt: readBack.receipt };
      if (pageNumber === 1) {
        const lastPage = linkRelationPage(readBack.exchange.response.headers, 'last');
        if (lastPage === undefined) return { status: 'unverifiable', receipt: readBack.receipt };
        pageNumber = lastPage;
      } else pageNumber--;
      if (pageNumber <= 1) break;
    }
    return { status: 'notFound', receipt: readBack.receipt };
  }

  /** Perform one read-back GET; transient failures retry and other refusals cannot verify. */
  async readCommentResponse(path: string, query: Record<string, string> = {}): Promise<CommentReadBack & { exchange?: Extract<GitHubExchange, { response: unknown }> }> {
    const exchange = await this.client.get(this.call, createIssueCommentOperationId, path, query);
    if (!('failure' in exchange)) return { status: 'found', receipt: exchange.receipt, exchange };
    if (exchange.isTransportFailure) return { status: 'retry', receipt: {}, failure: failure(createIssueCommentOperationId, 'TRANSPORT', 'GitHub comment read-back is temporarily unavailable'), retryAfterMs: 0 };
    if (exchange.branch === undefined) return { status: 'retry', receipt: exchange.receipt, failure: exchange.failure, retryAfterMs: exchange.retryAfterMs ?? 0 };
    return { status: 'unverifiable', receipt: exchange.receipt };
  }

  commentsPath(): string { return `${issuesPath(this.owner, this.repository)}/${this.requested.issueNumber}/comments`; }

  created(comment: GitHubIssueComment, commentReceipt: ReceiptDraft, isFoundByReadBack: boolean): MutationAttempt<CreatedIssueComment> {
    const output: CreatedIssueComment = { ...this.requested, commentId: comment.id };
    const url = safeURL(comment.htmlUrl); if (url) output.url = url;
    output.createdAt = comment.createdAt!;
    const receipt: ReceiptDraft = { ...commentReceipt, providerObjectId: String(comment.id), ...(isFoundByReadBack ? { metadata: { foundByReadBack: 'true' } } : {}) };
    return newMutationBranch('created', output, receipt);
  }

  branch(branch: string, kind: Failure['kind'], message: string, receipt: ReceiptDraft): MutationAttempt<CreatedIssueComment> {
    return newMutationBranch(branch, this.requested, receipt, failure(createIssueCommentOperationId, kind, message));
  }

  uncertain(kind: Failure['kind'], message: string, receipt: ReceiptDraft): MutationAttempt<CreatedIssueComment> {
    return newMutationUncertain(this.requested, failure(createIssueCommentOperationId, kind, message), receipt);
  }

  /** The send cap the Fence applies: the Step's override for a limited operation, else the manifest cap. */
  maxSends(): number {
    const definitionMaxSends = CreateIssueCommentDefinition.maxSends ?? 1;
    return definitionMaxSends >= 2 ? this.call.maxSendsOverride ?? definitionMaxSends : 1;
  }
}

/** Validate input, set the writer's repository, and append the call marker; a string selects defect. */
function buildCreateIssueCommentRequest(input: CreateIssueCommentInput, writer: CommentWriter): { body: string } | string {
  const repository = validateRepository(createIssueCommentOperationId, input.owner, input.repository);
  if (!Array.isArray(repository)) return repository.message;
  [writer.owner, writer.repository] = repository;
  if (!Number.isSafeInteger(input.issueNumber) || input.issueNumber <= 0) return 'GitHub issue number must be positive';
  const body = String(input.body ?? '');
  if (!body.trim()) return 'GitHub comment body is required';
  const markerSuffix = `\n\n${writer.marker}`;
  if (!isWellFormedText(body) || body.includes('\u0000') || [...body].length + [...markerSuffix].length > maximumIssueBodyInputCharacters) {
    return 'GitHub comment body must be valid text of at most 65536 characters including the connector marker';
  }
  return { body: body + markerSuffix };
}

/** Decode one comment with the field types the Go connector's JSON model accepts. */
function decodeGitHubIssueComment(value: unknown): GitHubIssueComment {
  const source = record(value);
  const user = source.user == null ? undefined : record(source.user);
  return {
    id: integerField(source, 'id'), body: stringField(source, 'body'), htmlUrl: stringField(source, 'html_url'),
    userLogin: user === undefined ? undefined : stringField(user, 'login'), createdAt: timestamp(source.created_at),
  };
}

function isValidCreatedComment(comment: GitHubIssueComment): boolean {
  return comment.id > 0 && comment.createdAt !== undefined && comment.createdAt !== zeroTime;
}

/** Return the page of a Link relation, zero when GitHub sends none, or undefined for an invalid link. */
function linkRelationPage(headers: Record<string, string | string[] | undefined>, relation: string): number | undefined {
  for (const [name, values] of Object.entries(headers)) {
    if (name.toLowerCase() !== 'link') continue;
    for (const link of Array.isArray(values) ? values : [values ?? '']) {
      for (const match of link.matchAll(/<([^>]*)>\s*;\s*rel="([^"]*)"/g)) {
        if (!match[2]!.split(/\s+/).some(candidate => candidate.toLowerCase() === relation)) continue;
        let target: URL;
        try { target = new URL(match[1]!, 'https://api.github.com'); } catch { return undefined; }
        const raw = target.searchParams.get('page');
        if (!raw || !/^\d+$/.test(raw) || !Number.isSafeInteger(Number(raw)) || Number(raw) <= 0) return undefined;
        return Number(raw);
      }
    }
  }
  return 0;
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
