// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newMutationBranch, newMutationRetry, newQueryBranch, newQueryRetry, type Call, type Failure, type Mutation, type MutationAttempt, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import type { SentryClient, SentryExchange } from './client.js';
import { GetIssueDefinition, ListIssuesDefinition, UpdateIssueStatusDefinition } from './generated/definitions.js';
import type { GetIssueInput, Issue, IssuePage, ListIssuesInput, UpdateIssueStatusInput } from './generated/models.js';

const emptyIssue: Issue = { id: '', shortId: '', title: '', status: '' };
const emptyPage: IssuePage = { issues: [] };

/** Query one organization issue page. */
export class ListIssuesOperation implements Query<ListIssuesInput, IssuePage> {
  /** Bind one Sentry client. */ constructor(private readonly client: SentryClient) {}
  /** Return the manifest definition. */ definition() { return ListIssuesDefinition; }
  /** Validate and invoke one list attempt. */
  async invoke(call: Call, input: ListIssuesInput): Promise<QueryAttempt<IssuePage>> {
    if (!input.organization) return queryFailure('listIssues', 'defect', 'VALIDATION', 'organization slug is required', emptyPage);
    const path = `/organizations/${pathEscape(input.organization)}/issues/${input.query ? `?query=${new URLSearchParams({ query: input.query }).toString().slice(6)}` : ''}`;
    const exchange = await this.client.exchange(call, 'listIssues', 'GET', path);
    if ('branch' in exchange) return queryExchange(exchange, emptyPage);
    try {
      const value: unknown = JSON.parse(Buffer.from(exchange.body).toString('utf8'));
      if (!Array.isArray(value)) throw new Error('issues missing');
      return newQueryBranch('listed', { issues: value.map(decodeIssue) }, {});
    } catch { return queryFailure('listIssues', 'invalidResponse', 'PROTOCOL', 'provider response is incomplete or malformed', emptyPage); }
  }
}

/** Query one organization issue. */
export class GetIssueOperation implements Query<GetIssueInput, Issue> {
  /** Bind one Sentry client. */ constructor(private readonly client: SentryClient) {}
  /** Return the manifest definition. */ definition() { return GetIssueDefinition; }
  /** Validate and invoke one detail attempt. */
  async invoke(call: Call, input: GetIssueInput): Promise<QueryAttempt<Issue>> {
    if (!input.organization || !input.id) return queryFailure('getIssue', 'defect', 'VALIDATION', 'organization and issue ID are required', emptyIssue);
    const exchange = await this.client.exchange(call, 'getIssue', 'GET', issuePath(input), undefined, true);
    if ('branch' in exchange) return queryExchange(exchange, emptyIssue);
    try { return newQueryBranch('found', decodeIssue(JSON.parse(Buffer.from(exchange.body).toString('utf8'))), {}); }
    catch { return queryFailure('getIssue', 'invalidResponse', 'PROTOCOL', 'provider response is incomplete or malformed', emptyIssue); }
  }
}

/** Set one issue's absolute status. */
export class UpdateIssueStatusOperation implements Mutation<UpdateIssueStatusInput, Issue> {
  /** Bind one Sentry client. */ constructor(private readonly client: SentryClient) {}
  /** Return the manifest definition. */ definition() { return UpdateIssueStatusDefinition; }
  /** Derive the same stable key as Go from the Call ID. */ idempotencyKey(callId: string): string { return callId; }
  /** Validate and invoke one status update attempt. */
  async invoke(call: Call, input: UpdateIssueStatusInput): Promise<MutationAttempt<Issue>> {
    if (!input.organization || !input.id) return mutationFailure('defect', 'VALIDATION', 'organization and issue ID are required');
    if (!['resolved', 'unresolved', 'ignored'].includes(input.status)) return mutationFailure('defect', 'VALIDATION', 'status must be resolved, unresolved, or ignored');
    const body = Buffer.from(JSON.stringify({ status: input.status }), 'utf8');
    const exchange = await this.client.exchange(call, 'updateIssueStatus', 'PUT', issuePath(input), body, true);
    if ('branch' in exchange) return mutationExchange(exchange);
    try { return newMutationBranch('updated', decodeIssue(JSON.parse(Buffer.from(exchange.body).toString('utf8'))), {}); }
    catch { return mutationFailure('invalidResponse', 'PROTOCOL', 'provider response is incomplete or malformed'); }
  }
}

function issuePath(input: { organization: string; id: string }): string { return `/organizations/${pathEscape(input.organization)}/issues/${pathEscape(input.id)}/`; }
function pathEscape(value: string): string {
  return encodeURIComponent(value).replace(/[!'()*]/g, character => `%${character.charCodeAt(0).toString(16).toUpperCase()}`)
    .replace(/%(24|26|2B|3D|3A|40)/gi, encoded => String.fromCharCode(parseInt(encoded.slice(1), 16)));
}
function decodeIssue(value: unknown): Issue {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('invalid issue');
  const object = value as Record<string, unknown>;
  const field = (name: string): string => {
    const found = object[name];
    if (found === undefined || found === null) return '';
    if (typeof found !== 'string') throw new Error('invalid issue field');
    return found;
  };
  const issue = { id: field('id'), shortId: field('shortId'), title: field('title'), status: field('status') };
  if (!issue.id) throw new Error('issue ID missing');
  return issue;
}
function queryExchange<Output>(exchange: Exclude<SentryExchange, { body: Uint8Array }>, empty: Output): QueryAttempt<Output> {
  if (exchange.branch === 'retry') return newQueryRetry(exchange.failure, exchange.retryAfterMs);
  return newQueryBranch(exchange.branch === 'notFound' ? 'notFound' : exchange.branch, empty, {}, exchange.failure);
}
function mutationExchange(exchange: Exclude<SentryExchange, { body: Uint8Array }>): MutationAttempt<Issue> {
  if (exchange.branch === 'retry') return newMutationRetry(exchange.failure, exchange.retryAfterMs);
  return newMutationBranch(exchange.branch, emptyIssue, {}, exchange.failure);
}
function queryFailure<Output>(operation: string, branch: string, kind: Failure['kind'], message: string, empty: Output): QueryAttempt<Output> {
  return newQueryBranch(branch, empty, {}, { kind, provider: 'sentry', operation, message });
}
function mutationFailure(branch: string, kind: Failure['kind'], message: string): MutationAttempt<Issue> {
  return newMutationBranch(branch, emptyIssue, {}, { kind, provider: 'sentry', operation: 'updateIssueStatus', message });
}
