// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import emailAddresses from 'email-addresses';
import { newQueryBranch, newQueryRetry, type Call, type Query, type QueryAttempt, type ReceiptDraft } from '@superdurable/connectors-sdk';
import { GetAuthenticatedProfileDefinition, ListPublicRepositoriesDefinition, ListMergedPullRequestsDefinition, ListPullRequestFilesDefinition, ListReleasesDefinition, ListCommitsDefinition } from './generated/definitions.js';
import type { GetAuthenticatedProfileInput, AuthenticatedProfile, ListPublicRepositoriesInput, PublicRepositories, Repository, ListMergedPullRequestsInput, MergedPullRequestPage, MergedPullRequest, ListPullRequestFilesInput, PullRequestFilePage, PullRequestFile, ListReleasesInput, ReleasePage, Release, ListCommitsInput, CommitPage, CommitSummary } from './generated/models.js';
import { failure, hasRequiredScopes, pathEscape, responseHeader, type GitHubClient, type GitHubExchange } from './client.js';

const emptyProfile: AuthenticatedProfile = { subject: '', login: '', verifiedEmail: '', publicRepos: 0, followers: 0, following: 0 };
const emptyRepositories: PublicRepositories = { repositories: [], truncated: false };
const emptyPullRequests: MergedPullRequestPage = { pullRequests: [], totalCount: 0, incompleteResults: false, nextPage: 0 };
const emptyFiles: PullRequestFilePage = { files: [], nextPage: 0 };
const emptyReleases: ReleasePage = { releases: [], nextPage: 0 };
const emptyCommits: CommitPage = { commits: [], nextPage: 0 };
const loginPattern = /^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$/;
const repositoryPattern = /^[A-Za-z0-9._-]{1,100}$/;
const shaPattern = /^(?:[0-9a-f]{40}|[0-9a-f]{64})$/;
const encoder = new TextEncoder();

type Terminal = { branch: string; failure: ReturnType<typeof failure>; receipt: ReceiptDraft };
function invalid<Input>(operation: string, output: Input, message: string): QueryAttempt<Input> {
  return newQueryBranch('defect', output, {}, failure(operation, 'VALIDATION', message));
}
function isTerminal(exchange: GitHubExchange): exchange is Terminal { return 'failure' in exchange && exchange.branch !== undefined; }
function classify<Input>(operation: string, output: Input, exchange: GitHubExchange): QueryAttempt<Input> | undefined {
  if ('failure' in exchange) {
    if (isTerminal(exchange)) return newQueryBranch(exchange.branch, output, exchange.receipt, exchange.failure);
    return newQueryRetry(exchange.failure, exchange.retryAfterMs ?? 0);
  }
  if (!hasRequiredScopes(exchange.response.headers)) return newQueryBranch('insufficientScope', output, exchange.receipt, failure(operation, 'AUTHORIZATION', 'GitHub OAuth grant does not match required scopes'));
  return undefined;
}
function decode(body: Uint8Array): unknown { return JSON.parse(Buffer.from(body).toString('utf8'), (key, value: unknown, context?: { source?: string }) => { if (typeof value === 'number' && ['id', 'number', 'public_repos', 'followers', 'following', 'stargazers_count', 'forks_count', 'open_issues_count', 'total_count', 'additions', 'deletions', 'changes'].includes(key) && !/^-?\d+$/.test(context?.source ?? '')) throw new TypeError('invalid GitHub integer'); return value; }) as unknown; }
function record(value: unknown): Record<string, unknown> { if (!value || typeof value !== 'object' || Array.isArray(value)) throw new TypeError('invalid GitHub object'); return value as Record<string, unknown>; }
function stringField(source: Record<string, unknown>, key: string): string { const value = source[key]; if (value == null) return ''; if (typeof value !== 'string') throw new TypeError(`invalid GitHub ${key}`); return value; }
function integerField(source: Record<string, unknown>, key: string): number { const value = source[key]; if (value == null) return 0; if (!Number.isSafeInteger(value)) throw new TypeError(`invalid GitHub ${key}`); return value as number; }
function booleanField(source: Record<string, unknown>, key: string): boolean { const value = source[key]; if (value == null) return false; if (typeof value !== 'boolean') throw new TypeError(`invalid GitHub ${key}`); return value; }
function validateUser(source: Record<string, unknown>): void {
  for (const key of ['login', 'name', 'avatar_url', 'html_url', 'company', 'blog', 'location', 'bio', 'twitter_username']) stringField(source, key);
  for (const key of ['id', 'public_repos', 'followers', 'following']) integerField(source, key);
  timestamp(source.created_at); timestamp(source.updated_at);
}
function validateRepositoryFields(source: Record<string, unknown>): void {
  for (const key of ['name', 'full_name', 'visibility', 'html_url', 'description', 'homepage', 'language']) stringField(source, key);
  for (const key of ['id', 'stargazers_count', 'forks_count', 'open_issues_count']) integerField(source, key);
  for (const key of ['private', 'fork', 'archived', 'disabled']) booleanField(source, key);
  for (const key of ['created_at', 'updated_at', 'pushed_at']) timestamp(source[key]);
  if (source.topics != null) {
    if (!Array.isArray(source.topics) || source.topics.some(item => typeof item !== 'string')) throw new TypeError('invalid GitHub topics');
  }
}
function bounded(value: string, maximumBytes: number): string {
  const trimmed = value.trim();
  if (encoder.encode(trimmed).length <= maximumBytes) return trimmed;
  let result = '';
  for (const character of trimmed) { if (encoder.encode(result + character).length > maximumBytes) break; result += character; }
  return result;
}
function safeURL(value: string): string {
  const boundedValue = bounded(value, 2048);
  try { const url = new URL(boundedValue); return (url.protocol === 'https:' || url.protocol === 'http:') && url.hostname ? url.toString() : ''; } catch { return ''; }
}
function nonNegative(value: number): number { return Math.max(value, 0); }
function timestamp(value: unknown, required = false): string | undefined {
  if (value == null) { if (required) throw new TypeError('missing GitHub timestamp'); return undefined; }
  if (typeof value !== 'string') throw new TypeError('invalid GitHub timestamp');
  const match = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:[.,](\d+))?(Z|[+-]\d{2}:\d{2})$/.exec(value);
  if (!match) throw new TypeError('invalid GitHub timestamp');
  const year = Number(match[1]), month = Number(match[2]), day = Number(match[3]), hour = Number(match[4]), minute = Number(match[5]), second = Number(match[6]);
  const leap = year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0);
  const days = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
  if (month < 1 || month > 12 || day < 1 || day > days[month - 1]! || hour > 23 || minute > 59 || second > 59 || (match[8] !== 'Z' && Number(match[8]!.slice(4)) > 59)) throw new TypeError('invalid GitHub timestamp');
  const date = new Date(0);
  date.setUTCFullYear(year, month - 1, day);
  date.setUTCHours(hour, minute, second, 0);
  const offset = match[8]!;
  if (offset !== 'Z') {
    const minutes = (Number(offset.slice(1, 3)) * 60 + Number(offset.slice(4))) * (offset[0] === '+' ? 1 : -1);
    date.setTime(date.getTime() - minutes * 60_000);
  }
  if (!Number.isFinite(date.getTime()) || date.getUTCFullYear() < 0 || date.getUTCFullYear() > 9999) throw new TypeError('invalid GitHub timestamp');
  const fraction = (match[7] ?? '').slice(0, 9).replace(/0+$/, '');
  return `${date.toISOString().slice(0, 19)}${fraction ? `.${fraction}` : ''}Z`;
}
function truncateCharacters(value: string, maximum: number): [string, boolean] { const characters = Array.from(value); return [characters.slice(0, maximum).join(''), characters.length > maximum]; }
function repo(owner: string, repository: string, operation: string): [string, string] | QueryAttempt<never> {
  const trimmedOwner = owner.trim(), trimmedRepository = repository.trim();
  if (!loginPattern.test(trimmedOwner)) return invalid(operation, undefined as never, 'GitHub repository owner is invalid');
  if (!repositoryPattern.test(trimmedRepository) || trimmedRepository === '.' || trimmedRepository === '..') return invalid(operation, undefined as never, 'GitHub repository name is invalid');
  return [trimmedOwner, trimmedRepository];
}
function page(pageSize: number | undefined, pageNumber: number | undefined, defaultSize: number, operation: string): [number, number] | QueryAttempt<never> {
  const size = pageSize || defaultSize, number = pageNumber || 1;
  if (!Number.isSafeInteger(size) || !Number.isSafeInteger(number) || size < 1 || size > 100 || number < 1) return invalid(operation, undefined as never, 'page size must be 1 through 100 and page must be positive');
  return [size, number];
}
function window(start: string, end: string, subject: string, operation: string): QueryAttempt<never> | undefined {
  try { const from = timestamp(start, true)!, to = timestamp(end, true)!; if (new Date(from).getTime() < new Date(to).getTime()) return; } catch { /* Invalid dates select the same defect branch. */ }
  return invalid(operation, undefined as never, `${subject} window must have a start before its end`);
}
function nextPage(headers: Record<string, string | string[] | undefined>, current: number): number {
  for (const [name, values] of Object.entries(headers)) {
    if (name.toLowerCase() !== 'link') continue;
    for (const link of Array.isArray(values) ? values : [values ?? '']) {
      for (const match of link.matchAll(/<([^>]*)>\s*;\s*rel="([^"]*)"/g)) {
        if (!match[2]!.split(/\s+/).some(relation => relation.toLowerCase() === 'next')) continue;
        let target: URL;
        try { target = new URL(match[1]!, 'https://api.github.com'); } catch { throw new TypeError('invalid GitHub page link'); }
        const raw = target.searchParams.get('page');
        if (!raw || !/^[+-]?\d+$/.test(raw) || !Number.isSafeInteger(Number(raw))) throw new TypeError('invalid GitHub page link');
        const next = Number(raw);
        if (next <= current) throw new TypeError('invalid GitHub page link');
        return next;
      }
    }
  }
  return 0;
}
function invalidResponse<Input>(operation: string, output: Input, message: string, receipt: ReceiptDraft): QueryAttempt<Input> { return newQueryBranch('invalidResponse', output, receipt, failure(operation, 'PROTOCOL', message)); }
function commonInput(input: { owner: string; repository: string; pageSize?: number | undefined; page?: number | undefined }, operation: string, defaultSize: number, output: unknown): [string, string, number, number] | QueryAttempt<unknown> {
  const pair = repo(input.owner, input.repository, operation);
  if (!Array.isArray(pair)) return invalid(operation, output, pair.failure?.message ?? 'GitHub repository is invalid');
  const paging = page(input.pageSize, input.page, defaultSize, operation);
  if (!Array.isArray(paging)) return invalid(operation, output, paging.failure?.message ?? 'invalid page');
  return [pair[0], pair[1], paging[0], paging[1]];
}

/** Authenticated GitHub user and verified primary email query. */
export class GetAuthenticatedProfileOperation implements Query<GetAuthenticatedProfileInput, AuthenticatedProfile> {
  definition() { return GetAuthenticatedProfileDefinition; }
  constructor(private readonly client: GitHubClient) {}
  /** Read both profile and verified email endpoints. */
  async invoke(call: Call, input: GetAuthenticatedProfileInput): Promise<QueryAttempt<AuthenticatedProfile>> {
    void input;
    const operation = 'getAuthenticatedProfile';
    const userExchange = await this.client.get(call, operation, '/user');
    const userResult = classify(operation, emptyProfile, userExchange);
    if (userResult) return userResult;
    if (!('response' in userExchange)) throw new TypeError('unclassified GitHub response');
    let user: Record<string, unknown>;
    try { user = record(decode(userExchange.response.body)); validateUser(user); if (integerField(user, 'id') <= 0 || !stringField(user, 'login').trim()) throw new TypeError('invalid profile'); }
    catch { return invalidResponse(operation, emptyProfile, 'GitHub returned an invalid profile response', userExchange.receipt); }
    const emailExchange = await this.client.get(call, operation, '/user/emails', { per_page: '100' });
    const receipt: ReceiptDraft = { ...emailExchange.receipt, metadata: { profileRequestId: userExchange.receipt.providerRequestId ?? '', emailRequestId: emailExchange.receipt.providerRequestId ?? '' } };
    emailExchange.receipt = receipt;
    const emailResult = classify(operation, emptyProfile, emailExchange);
    if (emailResult) return emailResult;
    if (!('response' in emailExchange)) throw new TypeError('unclassified GitHub response');
    let primary = '';
    try {
      const emails: unknown = decode(emailExchange.response.body);
      if (!Array.isArray(emails)) throw new TypeError('invalid email list');
      for (const item of emails) {
        const candidate = record(item);
        const candidateEmail = stringField(candidate, 'email');
        if (!booleanField(candidate, 'primary') || !booleanField(candidate, 'verified')) continue;
        const email = candidateEmail.trim().toLowerCase();
        if (email.length <= 320 && isPlainEmail(email)) { primary = email; break; }
      }
    } catch { return invalidResponse(operation, emptyProfile, 'GitHub returned an invalid email response', receipt); }
    if (!primary) return newQueryBranch('verifiedEmailRequired', emptyProfile, receipt, failure(operation, 'AUTHENTICATION', 'GitHub primary verified email is required'));
    try {
      const profile: AuthenticatedProfile = { subject: String(integerField(user, 'id')), login: bounded(stringField(user, 'login'), 100), verifiedEmail: primary,
        publicRepos: nonNegative(integerField(user, 'public_repos')), followers: nonNegative(integerField(user, 'followers')), following: nonNegative(integerField(user, 'following')) };
      for (const [outputKey, providerKey, max] of [['name', 'name', 256], ['company', 'company', 256], ['location', 'location', 256], ['bio', 'bio', 1024], ['twitterUsername', 'twitter_username', 100]] as const) {
        const value = bounded(stringField(user, providerKey), max); if (value) Object.assign(profile, { [outputKey]: value });
      }
      for (const [outputKey, providerKey] of [['avatarUrl', 'avatar_url'], ['profileUrl', 'html_url'], ['blogUrl', 'blog']] as const) {
        const value = safeURL(stringField(user, providerKey)); if (value) Object.assign(profile, { [outputKey]: value });
      }
      const created = timestamp(user.created_at), updated = timestamp(user.updated_at);
      if (created) profile.createdAt = created; if (updated) profile.updatedAt = updated;
      return newQueryBranch('profileLoaded', profile, receipt);
    } catch { return invalidResponse(operation, emptyProfile, 'GitHub returned an invalid profile response', receipt); }
  }
}

/** Bounded public repository listing query. */
export class ListPublicRepositoriesOperation implements Query<ListPublicRepositoriesInput, PublicRepositories> {
  definition() { return ListPublicRepositoriesDefinition; }
  constructor(private readonly client: GitHubClient) {}
  /** Read pages until the requested bound or provider end. */
  async invoke(call: Call, input: ListPublicRepositoriesInput): Promise<QueryAttempt<PublicRepositories>> {
    const operation = 'listPublicRepositories', login = input.login.trim(), limit = input.limit || this.client.defaultRepositoryLimit;
    if (!loginPattern.test(login)) return invalid(operation, emptyRepositories, 'GitHub login is invalid');
    if (!Number.isSafeInteger(limit) || limit < 1 || limit > this.client.maxRepositories) return invalid(operation, emptyRepositories, 'repository limit is outside the configured range');
    const repositories = new Map<number, Repository>();
    let pageNumber = 1, truncated = false, lastReceipt: ReceiptDraft = {};
    for (;;) {
      const exchange = await this.client.get(call, operation, `/users/${pathEscape(login)}/repos`, { type: 'owner', sort: 'pushed', direction: 'desc', per_page: '100', page: String(pageNumber) });
      exchange.receipt = { ...exchange.receipt, metadata: { pagesRead: String(pageNumber) } };
      const result = classify(operation, emptyRepositories, exchange); if (result) return result;
      if (!('response' in exchange)) throw new TypeError('unclassified GitHub response');
      lastReceipt = exchange.receipt;
      let items: unknown;
      try { items = decode(exchange.response.body); if (!Array.isArray(items)) throw new TypeError('invalid repositories'); }
      catch { return invalidResponse(operation, emptyRepositories, 'GitHub returned an invalid repository response', lastReceipt); }
      try {
        for (const item of items as unknown[]) {
          const source = record(item); validateRepositoryFields(source); const id = integerField(source, 'id');
          if (id <= 0 || booleanField(source, 'private') || (stringField(source, 'visibility') && stringField(source, 'visibility') !== 'public') || repositories.has(id)) continue;
          if (repositories.size === limit) { truncated = true; break; }
          const value: Repository = { id, name: bounded(stringField(source, 'name'), 256), fullName: bounded(stringField(source, 'full_name'), 512),
            fork: booleanField(source, 'fork'), archived: booleanField(source, 'archived'), disabled: booleanField(source, 'disabled'),
            stargazersCount: nonNegative(integerField(source, 'stargazers_count')), forksCount: nonNegative(integerField(source, 'forks_count')), openIssuesCount: nonNegative(integerField(source, 'open_issues_count')) };
          for (const [outputKey, providerKey, max] of [['description', 'description', 1024], ['language', 'language', 128]] as const) { const text = bounded(stringField(source, providerKey), max); if (text) Object.assign(value, { [outputKey]: text }); }
          for (const [outputKey, providerKey] of [['profileUrl', 'html_url'], ['homepageUrl', 'homepage']] as const) { const text = safeURL(stringField(source, providerKey)); if (text) Object.assign(value, { [outputKey]: text }); }
          if (source.topics != null) { if (!Array.isArray(source.topics)) throw new TypeError('invalid topics'); const topics = source.topics.slice(0, 100).map(topic => { if (typeof topic !== 'string') throw new TypeError('invalid topic'); return bounded(topic, 64); }).filter(Boolean); if (topics.length) value.topics = topics; }
          for (const [outputKey, providerKey] of [['createdAt', 'created_at'], ['updatedAt', 'updated_at'], ['pushedAt', 'pushed_at']] as const) { const date = timestamp(source[providerKey]); if (date) Object.assign(value, { [outputKey]: date }); }
          repositories.set(id, value);
        }
      } catch { return invalidResponse(operation, emptyRepositories, 'GitHub returned an invalid repository response', lastReceipt); }
      const hasNext = responseHeader(exchange.response.headers, 'link').split(',').some(link => link.includes('rel="next"'));
      if (repositories.size === limit) { truncated ||= hasNext; break; }
      if (!hasNext || (items as unknown[]).length === 0) break;
      pageNumber++;
    }
    const values = [...repositories.values()].sort((left, right) => {
      const leftTime = left.pushedAt ? new Date(left.pushedAt).getTime() : 0, rightTime = right.pushedAt ? new Date(right.pushedAt).getTime() : 0;
      return rightTime - leftTime || left.id - right.id;
    });
    return newQueryBranch('repositoriesLoaded', { repositories: values, truncated }, lastReceipt);
  }
}

/** Search merged pull requests within a bounded time window. */
export class ListMergedPullRequestsOperation implements Query<ListMergedPullRequestsInput, MergedPullRequestPage> {
  definition() { return ListMergedPullRequestsDefinition; }
  constructor(private readonly client: GitHubClient) {}
  /** Read one search page and bound its text. */
  async invoke(call: Call, input: ListMergedPullRequestsInput): Promise<QueryAttempt<MergedPullRequestPage>> {
    const operation = 'listMergedPullRequests';
    const validated = commonInput(input, operation, 30, emptyPullRequests);
    if (!Array.isArray(validated)) return validated as QueryAttempt<MergedPullRequestPage>;
    const [owner, repository, size, pageNumber] = validated;
    const invalidWindow = window(input.mergedAfter, input.mergedBefore, 'merged pull request', operation);
    if (invalidWindow) return invalid(operation, emptyPullRequests, invalidWindow.failure?.message ?? 'invalid window');
    if (pageNumber > 1000 || (pageNumber - 1) * size >= 1000) return invalid(operation, emptyPullRequests, "search page starts after GitHub's first 1000 results");
    const providerQuery = `repo:${owner}/${repository} is:pr is:merged merged:${providerTime(input.mergedAfter)}..${providerTime(input.mergedBefore)}`;
    const exchange = await this.client.get(call, operation, '/search/issues', { q: providerQuery, sort: 'created', order: 'desc', per_page: String(size), page: String(pageNumber) });
    const result = classify(operation, emptyPullRequests, exchange); if (result) return result;
    if (!('response' in exchange)) throw new TypeError('unclassified GitHub response');
    try {
      const source = record(decode(exchange.response.body));
      if (!Number.isSafeInteger(source.total_count) || !Array.isArray(source.items)) throw new TypeError('invalid search response');
      const pullRequests: MergedPullRequest[] = source.items.map((item: unknown) => {
        const issue = record(item), pullRequest = record(issue.pull_request), number = integerField(issue, 'number');
        if (number <= 0) throw new TypeError('invalid merged pull request');
        const mergedAt = timestamp(pullRequest.merged_at, true)!;
        const [body, bodyTruncated] = truncateCharacters(stringField(issue, 'body'), this.client.maxPullRequestBodyCharacters);
        const labels = issue.labels; if (labels != null && !Array.isArray(labels)) throw new TypeError('invalid labels');
        const names = (labels as unknown[] | undefined ?? []).map(label => stringField(record(label), 'name')).slice(0, 100).map(name => bounded(name, 256)).filter(Boolean);
        const author = issue.user == null ? '' : bounded(stringField(record(issue.user), 'login'), 100);
        const value: MergedPullRequest = { number, title: bounded(stringField(issue, 'title'), 1024), bodyTruncated, mergedAt };
        if (body) value.body = body; if (names.length) value.labels = names; if (author) value.authorLogin = author;
        const url = safeURL(stringField(issue, 'html_url')); if (url) value.url = url;
        return value;
      });
      return newQueryBranch('listed', { pullRequests, totalCount: nonNegative(integerField(source, 'total_count')), incompleteResults: booleanField(source, 'incomplete_results'), nextPage: nextPage(exchange.response.headers, pageNumber) }, exchange.receipt);
    } catch { return invalidResponse(operation, emptyPullRequests, 'GitHub returned an invalid search response', exchange.receipt); }
  }
}

/** Read a single page of changed pull request files. */
export class ListPullRequestFilesOperation implements Query<ListPullRequestFilesInput, PullRequestFilePage> {
  definition() { return ListPullRequestFilesDefinition; }
  constructor(private readonly client: GitHubClient) {}
  /** Fetch and validate one changed-file page. */
  async invoke(call: Call, input: ListPullRequestFilesInput): Promise<QueryAttempt<PullRequestFilePage>> {
    const operation = 'listPullRequestFiles';
    const validated = commonInput(input, operation, 50, emptyFiles);
    if (!Array.isArray(validated)) return validated as QueryAttempt<PullRequestFilePage>;
    if (!Number.isSafeInteger(input.number) || input.number <= 0) return invalid(operation, emptyFiles, 'pull request number must be positive');
    const [owner, repository, size, pageNumber] = validated;
    const exchange = await this.client.get(call, operation, `/repos/${pathEscape(owner)}/${pathEscape(repository)}/pulls/${input.number}/files`, { per_page: String(size), page: String(pageNumber) });
    const result = classify(operation, emptyFiles, exchange); if (result) return result;
    if (!('response' in exchange)) throw new TypeError('unclassified GitHub response');
    try {
      const source = decode(exchange.response.body); if (!Array.isArray(source)) throw new TypeError('invalid changed-file response');
      const files: PullRequestFile[] = source.map(item => {
        const value = record(item), filename = stringField(value, 'filename'), previousFilename = stringField(value, 'previous_filename'), status = bounded(stringField(value, 'status'), 32);
        if (!validFilename(filename) || previousFilename && !validFilename(previousFilename) || !status) throw new TypeError('invalid changed file');
        const [patch, patchTruncated] = truncateCharacters(stringField(value, 'patch'), this.client.maxPatchCharacters);
        const file: PullRequestFile = { filename, status, additions: nonNegative(integerField(value, 'additions')), deletions: nonNegative(integerField(value, 'deletions')), changes: nonNegative(integerField(value, 'changes')), patchTruncated };
        if (previousFilename) file.previousFilename = previousFilename; if (patch) file.patch = patch;
        return file;
      });
      return newQueryBranch('listed', { files, nextPage: nextPage(exchange.response.headers, pageNumber) }, exchange.receipt);
    } catch { return invalidResponse(operation, emptyFiles, 'GitHub returned an invalid changed-file response', exchange.receipt); }
  }
}

/** Read one bounded page of repository releases. */
export class ListReleasesOperation implements Query<ListReleasesInput, ReleasePage> {
  definition() { return ListReleasesDefinition; }
  constructor(private readonly client: GitHubClient) {}
  /** Fetch one release page and preserve provider order. */
  async invoke(call: Call, input: ListReleasesInput): Promise<QueryAttempt<ReleasePage>> {
    const operation = 'listReleases';
    const validated = commonInput(input, operation, 30, emptyReleases);
    if (!Array.isArray(validated)) return validated as QueryAttempt<ReleasePage>;
    const maximum = input.maxBodyCharacters || 4000;
    if (!Number.isSafeInteger(maximum) || maximum < 1 || maximum > 16384) return invalid(operation, emptyReleases, 'maxBodyCharacters must be between 1 and 16384');
    const [owner, repository, size, pageNumber] = validated;
    const exchange = await this.client.get(call, operation, `/repos/${pathEscape(owner)}/${pathEscape(repository)}/releases`, { per_page: String(size), page: String(pageNumber) });
    const result = classify(operation, emptyReleases, exchange); if (result) return result;
    if (!('response' in exchange)) throw new TypeError('unclassified GitHub response');
    try {
      const source = decode(exchange.response.body); if (!Array.isArray(source) || source.length > size) throw new TypeError('invalid release page');
      const releases: Release[] = source.map(item => {
        const value = record(item), id = integerField(value, 'id'), tagName = stringField(value, 'tag_name'), draft = booleanField(value, 'draft');
        const createdAt = timestamp(value.created_at, true)!, publishedAt = timestamp(value.published_at);
        if (id <= 0 || !validQueryText(tagName, 255, false) || !tagName.trim() || !createdAt || (!draft && !publishedAt)) throw new TypeError('invalid release');
        const [body, bodyTruncated] = truncateCharacters(stringField(value, 'body'), maximum);
        const release: Release = { id, tagName, bodyTruncated, isDraft: draft, isPrerelease: booleanField(value, 'prerelease'), createdAt };
        const name = bounded(stringField(value, 'name'), 1024), url = safeURL(stringField(value, 'html_url'));
        if (name) release.name = name; if (url) release.url = url; if (body) release.body = body; if (publishedAt) release.publishedAt = publishedAt;
        return release;
      });
      return newQueryBranch('listed', { releases, nextPage: nextPage(exchange.response.headers, pageNumber) }, exchange.receipt);
    } catch { return invalidResponse(operation, emptyReleases, 'GitHub returned an invalid release page', exchange.receipt); }
  }
}

/** Read repository commits within a bounded time window. */
export class ListCommitsOperation implements Query<ListCommitsInput, CommitPage> {
  definition() { return ListCommitsDefinition; }
  constructor(private readonly client: GitHubClient) {}
  /** Fetch one commit page, including an empty repository's 409 result. */
  async invoke(call: Call, input: ListCommitsInput): Promise<QueryAttempt<CommitPage>> {
    const operation = 'listCommits';
    const validated = commonInput(input, operation, 30, emptyCommits);
    if (!Array.isArray(validated)) return validated as QueryAttempt<CommitPage>;
    const invalidWindow = window(input.since, input.until, 'commit', operation);
    if (invalidWindow) return invalid(operation, emptyCommits, invalidWindow.failure?.message ?? 'invalid window');
    if (!validQueryText(input.path ?? '', 4096, true)) return invalid(operation, emptyCommits, 'commit path is invalid');
    if (!validQueryText(input.ref ?? '', 255, false)) return invalid(operation, emptyCommits, 'commit ref is invalid');
    const [owner, repository, size, pageNumber] = validated;
    const query = { since: providerTime(input.since), until: providerTime(input.until), per_page: String(size), page: String(pageNumber), ...(input.path ? { path: input.path } : {}), ...(input.ref ? { sha: input.ref } : {}) };
    const exchange = await this.client.get(call, operation, `/repos/${pathEscape(owner)}/${pathEscape(repository)}/commits`, query);
    if ('response' in exchange && exchange.response.status === 409) {
      if (!hasRequiredScopes(exchange.response.headers)) return newQueryBranch('insufficientScope', emptyCommits, exchange.receipt, failure(operation, 'AUTHORIZATION', 'GitHub OAuth grant does not match required scopes'));
      return newQueryBranch('listed', emptyCommits, { ...exchange.receipt, metadata: { repositoryEmpty: 'true' } });
    }
    const result = classify(operation, emptyCommits, exchange); if (result) return result;
    if (!('response' in exchange)) throw new TypeError('unclassified GitHub response');
    try {
      const source = decode(exchange.response.body); if (!Array.isArray(source)) throw new TypeError('invalid commit response');
      const commits: CommitSummary[] = source.map(item => {
        const value = record(item), sha = stringField(value, 'sha'); if (!shaPattern.test(sha)) throw new TypeError('invalid SHA');
        const commit = record(value.commit), author = record(commit.author), committer = record(commit.committer);
        const authoredAt = timestamp(author.date, true)!, committedAt = timestamp(committer.date, true)!;
        const [message, messageTruncated] = truncateCharacters(stringField(commit, 'message'), this.client.maxCommitMessageCharacters);
        const summary: CommitSummary = { sha, message, messageTruncated, authoredAt, committedAt };
        const authorName = bounded(stringField(author, 'name'), 256), authorLogin = value.author == null ? '' : bounded(stringField(record(value.author), 'login'), 100), url = safeURL(stringField(value, 'html_url'));
        if (authorName) summary.authorName = authorName; if (authorLogin) summary.authorLogin = authorLogin; if (url) summary.url = url;
        return summary;
      });
      return newQueryBranch('listed', { commits, nextPage: nextPage(exchange.response.headers, pageNumber) }, exchange.receipt);
    } catch { return invalidResponse(operation, emptyCommits, 'GitHub returned an invalid commit response', exchange.receipt); }
  }
}

function validFilename(value: string): boolean { return value !== '' && encoder.encode(value).length <= 4096 && !value.includes('\0'); }
function validQueryText(value: string, maximumBytes: number, allowSpace: boolean): boolean { return encoder.encode(value).length <= maximumBytes && !/\p{Cc}/u.test(value) && (allowSpace || !/\s/u.test(value)); }
function providerTime(value: string): string { return new Date(timestamp(value, true)!).toISOString().replace(/\.\d{3}Z$/, 'Z'); }

function isPlainEmail(value: string): boolean {
  const address = emailAddresses.parseOneAddress({ input: value, rfc6532: true });
  return address?.type === 'mailbox' && address.address === value && value.includes('@');
}
