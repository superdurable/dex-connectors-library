// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newQueryBranch, newQueryRetry, type Call, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import type { BitbucketClient, ReadResult } from './client.js';
import { ListPullRequestsDefinition, ListRepositoriesDefinition } from './generated/definitions.js';
import type { ListPullRequestsInput, ListRepositoriesInput, PullRequest, PullRequestPage, Repository, RepositoryPage } from './generated/models.js';

const zeroRepositories: RepositoryPage = { values: [], next: '', page: 0, pagelen: 0 };
const zeroPullRequests: PullRequestPage = { values: [], next: '', page: 0, pagelen: 0 };

/** Query that reads one repository page. */
export class ListRepositoriesOperation implements Query<ListRepositoriesInput, RepositoryPage> {
  /** Bind one Bitbucket client. */ constructor(private readonly client: BitbucketClient) {}
  /** Return the manifest definition. */ definition() { return ListRepositoriesDefinition; }
  /** Validate the workspace and read one page. */
  async invoke(call: Call, input: ListRepositoriesInput): Promise<QueryAttempt<RepositoryPage>> {
    if (!validSegment(input.workspace)) return failed('listRepositories', 'defect', 'VALIDATION', 'workspace must be a path segment', zeroRepositories);
    const result = await this.client.read(call, 'listRepositories', `/repositories/${escapePathSegment(input.workspace)}`);
    if (!('response' in result)) return readFailure(result, zeroRepositories);
    try { return newQueryBranch('listed', decodeRepositoryPage(result.response.body)); }
    catch { return failed('listRepositories', 'invalidResponse', 'PROTOCOL', 'provider returned invalid JSON', zeroRepositories); }
  }
}

/** Query that reads one pull request page. */
export class ListPullRequestsOperation implements Query<ListPullRequestsInput, PullRequestPage> {
  /** Bind one Bitbucket client. */ constructor(private readonly client: BitbucketClient) {}
  /** Return the manifest definition. */ definition() { return ListPullRequestsDefinition; }
  /** Validate both path segments and read one page. */
  async invoke(call: Call, input: ListPullRequestsInput): Promise<QueryAttempt<PullRequestPage>> {
    if (!validSegment(input.workspace)) return failed('listPullRequests', 'defect', 'VALIDATION', 'workspace must be a path segment', zeroPullRequests);
    if (!validSegment(input.repository)) return failed('listPullRequests', 'defect', 'VALIDATION', 'repository must be a path segment', zeroPullRequests);
    const path = `/repositories/${escapePathSegment(input.workspace)}/${escapePathSegment(input.repository)}/pullrequests`;
    const result = await this.client.read(call, 'listPullRequests', path);
    if (!('response' in result)) return readFailure(result, zeroPullRequests);
    try { return newQueryBranch('listed', decodePullRequestPage(result.response.body)); }
    catch { return failed('listPullRequests', 'invalidResponse', 'PROTOCOL', 'provider returned invalid JSON', zeroPullRequests); }
  }
}

function validSegment(value: string): boolean {
  return typeof value === 'string' && value !== '' && value !== '.' && value !== '..' && !/[/?#]/.test(value) && ![...value].some(character => character.charCodeAt(0) < 32);
}

function escapePathSegment(value: string): string {
  return encodeURIComponent(value).replace(/[!'()*]/g, character => `%${character.charCodeAt(0).toString(16).toUpperCase()}`).replace(/%24|%26|%2B|%3D|%3A|%40/gi, encoded => String.fromCharCode(Number.parseInt(encoded.slice(1), 16)));
}

function readFailure<T>(result: Exclude<ReadResult, { response: unknown }>, zero: T): QueryAttempt<T> {
  if ('retry' in result) return newQueryRetry(result.failure, result.retryAfterMs);
  return newQueryBranch(result.branch, zero, {}, result.failure);
}

function failed<T>(operation: string, branch: string, kind: 'VALIDATION' | 'PROTOCOL', message: string, zero: T): QueryAttempt<T> {
  return newQueryBranch(branch, zero, {}, { kind, provider: 'bitbucket', operation, message });
}

function decodePage(body: Uint8Array): Record<string, unknown> {
  const value: unknown = JSON.parse(Buffer.from(body).toString('utf8'));
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new TypeError('invalid page');
  const page = value as Record<string, unknown>;
  if (!Array.isArray(page.values)) throw new TypeError('missing values');
  return page;
}

function goString(value: unknown): string { if (value === undefined || value === null) return ''; if (typeof value !== 'string') throw new TypeError('invalid string'); return value; }
function goInteger(value: unknown): number { if (value === undefined || value === null) return 0; if (typeof value !== 'number' || !Number.isSafeInteger(value)) throw new TypeError('invalid integer'); return value; }
function goBoolean(value: unknown): boolean { if (value === undefined || value === null) return false; if (typeof value !== 'boolean') throw new TypeError('invalid boolean'); return value; }
function goObject(value: unknown): Record<string, unknown> { if (value === null) return {}; if (typeof value !== 'object' || Array.isArray(value)) throw new TypeError('invalid item'); return value as Record<string, unknown>; }

function decodeRepositoryPage(body: Uint8Array): RepositoryPage {
  const page = decodePage(body);
  return { values: (page.values as unknown[]).map(item => {
    const repository = goObject(item);
    return { uuid: goString(repository.uuid), name: goString(repository.name), full_name: goString(repository.full_name), is_private: goBoolean(repository.is_private) } satisfies Repository;
  }), next: goString(page.next), page: goInteger(page.page), pagelen: goInteger(page.pagelen) };
}

function decodePullRequestPage(body: Uint8Array): PullRequestPage {
  const page = decodePage(body);
  return { values: (page.values as unknown[]).map(item => {
    const pullRequest = goObject(item);
    return { id: goInteger(pullRequest.id), title: goString(pullRequest.title), state: goString(pullRequest.state) } satisfies PullRequest;
  }), next: goString(page.next), page: goInteger(page.page), pagelen: goInteger(page.pagelen) };
}
