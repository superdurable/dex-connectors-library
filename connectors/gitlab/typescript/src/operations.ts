// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newMutationBranch, newMutationRetry, newQueryBranch, newQueryRetry, type Call, type Failure, type Mutation, type MutationAttempt, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import type { GitLabClient, GitLabExchange } from './client.js';
import { pathEscape } from './client.js';
import { GetProjectDefinition, ListIssuesDefinition, UpdateIssueLabelsDefinition } from './generated/definitions.js';
import type { GetProjectInput, Issue, IssuesPage, ListIssuesInput, Project, UpdateIssueLabelsInput } from './generated/models.js';

const emptyProject: Project = { id: 0, name: '', path_with_namespace: '', web_url: '' };
const emptyIssue: Issue = { id: 0, iid: 0, title: '', labels: [], web_url: '' };
const emptyPage: IssuesPage = { issues: [] };

/** Read one GitLab project by ID or namespace path. */
export class GetProjectOperation implements Query<GetProjectInput, Project> {
  /** Bind the authenticated client. */ constructor(private readonly client: GitLabClient) {}
  /** Return the manifest definition. */ definition() { return GetProjectDefinition; }
  /** Validate and classify one project lookup. */
  async invoke(call: Call, input: GetProjectInput): Promise<QueryAttempt<Project>> {
    if (!input.projectId?.trim()) return queryDefect('getProject', emptyProject, 'projectId is required');
    const result = await this.client.exchange(call, 'getProject', 'GET', `/projects/${pathEscape(input.projectId)}`);
    if (!('response' in result)) return queryOutcome(result, emptyProject);
    try {
      const value = JSON.parse(Buffer.from(result.response.body).toString('utf8')) as Project;
      if (!value || typeof value !== 'object' || !Number.isSafeInteger(value.id) || value.id <= 0) throw new Error('missing project ID');
      return newQueryBranch('found', { id: value.id, name: stringField(value.name), path_with_namespace: stringField(value.path_with_namespace), web_url: stringField(value.web_url) }, {});
    } catch { return queryFailure('getProject', 'invalidResponse', emptyProject); }
  }
}

/** Read the first issue page for one project. */
export class ListIssuesOperation implements Query<ListIssuesInput, IssuesPage> {
  /** Bind the authenticated client. */ constructor(private readonly client: GitLabClient) {}
  /** Return the manifest definition. */ definition() { return ListIssuesDefinition; }
  /** Validate and classify one issue list. */
  async invoke(call: Call, input: ListIssuesInput): Promise<QueryAttempt<IssuesPage>> {
    if (!input.projectId?.trim()) return queryDefect('listIssues', emptyPage, 'projectId is required');
    const result = await this.client.exchange(call, 'listIssues', 'GET', `/projects/${pathEscape(input.projectId)}/issues`);
    if (!('response' in result)) return queryOutcome(result, emptyPage);
    try {
      const values: unknown = JSON.parse(Buffer.from(result.response.body).toString('utf8'));
      if (!Array.isArray(values)) throw new Error('missing issues');
      return newQueryBranch('listed', { issues: values.map(decodeIssue) }, {});
    } catch { return queryFailure('listIssues', 'invalidResponse', emptyPage); }
  }
}

/** Replace the complete label set on a GitLab issue. */
export class UpdateIssueLabelsOperation implements Mutation<UpdateIssueLabelsInput, Issue> {
  /** Bind the authenticated client. */ constructor(private readonly client: GitLabClient) {}
  /** Return the manifest definition. */ definition() { return UpdateIssueLabelsDefinition; }
  /** Reuse the Dex Call ID because absolute replacement is repeat safe. */ idempotencyKey(callId: string) { return callId; }
  /** Validate and classify one absolute label replacement. */
  async invoke(call: Call, input: UpdateIssueLabelsInput): Promise<MutationAttempt<Issue>> {
    if (!input.projectId?.trim() || !Number.isSafeInteger(input.issueIid) || input.issueIid <= 0) return mutationDefect('updateIssueLabels', 'projectId and positive issueIid are required');
    const labels = input.labels ?? [];
    if (!Array.isArray(labels) || labels.some(label => typeof label !== 'string' || !label.trim() || label.includes(','))) return mutationDefect('updateIssueLabels', 'labels must be nonblank and cannot contain commas');
    const form = new URLSearchParams({ labels: labels.join(',') }).toString();
    const result = await this.client.exchange(call, 'updateIssueLabels', 'PUT', `/projects/${pathEscape(input.projectId)}/issues/${input.issueIid}`, form);
    if (!('response' in result)) return mutationOutcome(result);
    try {
      const value = decodeIssue(JSON.parse(Buffer.from(result.response.body).toString('utf8')) as unknown);
      if (value.id <= 0 || value.iid <= 0) throw new Error('missing issue identity');
      return newMutationBranch('updated', value, {});
    } catch { return mutationFailure('updateIssueLabels', 'invalidResponse', 'PROTOCOL', 'response was malformed'); }
  }
}

function stringField(value: unknown): string {
  if (value === undefined || value === null) return '';
  if (typeof value !== 'string') throw new Error('invalid string field');
  return value;
}
function integerField(value: unknown): number {
  if (value === undefined || value === null) return 0;
  if (typeof value !== 'number' || !Number.isSafeInteger(value)) throw new Error('invalid integer field');
  return value;
}
function decodeIssue(input: unknown): Issue {
  if (!input || typeof input !== 'object') throw new Error('invalid issue');
  const issue = input as Record<string, unknown>;
  if (issue.labels !== undefined && issue.labels !== null && (!Array.isArray(issue.labels) || issue.labels.some(label => typeof label !== 'string'))) throw new Error('invalid labels');
  return { id: integerField(issue.id), iid: integerField(issue.iid), title: stringField(issue.title), labels: issue.labels ?? [], web_url: stringField(issue.web_url) } as Issue;
}
function failure(operation: string, kind: Failure['kind'], message: string): Failure { return { kind, provider: 'gitlab', operation, message }; }
function queryDefect<T>(operation: string, empty: T, message: string): QueryAttempt<T> { return newQueryBranch('defect', empty, {}, failure(operation, 'VALIDATION', message)); }
function mutationDefect(operation: string, message: string): MutationAttempt<Issue> { return newMutationBranch('defect', emptyIssue, {}, failure(operation, 'VALIDATION', message)); }
function queryFailure<T>(operation: string, branch: string, empty: T): QueryAttempt<T> { return newQueryBranch(branch, empty, {}, failure(operation, 'PROTOCOL', 'response was malformed')); }
function mutationFailure(operation: string, branch: string, kind: Failure['kind'], message: string): MutationAttempt<Issue> { return newMutationBranch(branch, emptyIssue, {}, failure(operation, kind, message)); }
function queryOutcome<T>(result: Exclude<GitLabExchange, { response: unknown }>, empty: T): QueryAttempt<T> {
  if (result.branch === 'retry') return newQueryRetry(result.failure, result.retryAfterMs);
  return newQueryBranch(result.branch, empty, {}, result.failure);
}
function mutationOutcome(result: Exclude<GitLabExchange, { response: unknown }>): MutationAttempt<Issue> {
  if (result.branch === 'retry') return newMutationRetry(result.failure, result.retryAfterMs);
  return newMutationBranch(result.branch, emptyIssue, {}, result.failure);
}
