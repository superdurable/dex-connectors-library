// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListProjectsDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListProjectsInput, ListProjectsOutput, Project } from './generated/models.js';

const tokenEndpoint = 'https://dev.worktile.com/open-api/tenant-access-token';
const projectsEndpoint = 'https://dev.worktile.com/open-api/mission/get-projects-by-page';
const emptyOutput: ListProjectsOutput = { pageIndex: 0, pageSize: 0, hasMore: false, projects: [] };
/** Sender used for Worktile token and project requests. */
export type WorktileRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;
/** Optional provider transport for deterministic tests. */
export interface WorktileClientOptions { /** Production uses the SDK sender. */ send?: WorktileRequestSender; }
/** Query one page of Worktile tenant projects. */
export class WorktileClient {
  readonly timeoutMs: number;
  readonly maxResponseBytes: number;
  readonly credentials: CredentialProvider<Credentials>;
  readonly send: WorktileRequestSender;
  /** Validate bounded token and project requests. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: WorktileClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Worktile configuration and credentials are required');
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > 16 * 1024 * 1024) throw new TypeError('Worktile limits must be positive and bounded');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the project-list Query. */ listProjects(): ListProjectsOperation { return new ListProjectsOperation(this); }
}
/** Obtain a short-lived token and list a project page without persisting the token. */
export class ListProjectsOperation implements Query<ListProjectsInput, ListProjectsOutput> {
  /** Bind the Query client. */ constructor(private readonly client: WorktileClient) {}
  /** Stable manifest definition. */ definition(): QueryDefinition { return ListProjectsDefinition; }
  /** Execute token and project requests, classifying either response. */
  async invoke(call: Call, input: ListProjectsInput): Promise<QueryAttempt<ListProjectsOutput>> {
    if (!Number.isSafeInteger(input.pageIndex) || input.pageIndex < 0 || !Number.isSafeInteger(input.pageSize) || input.pageSize < 1 || input.pageSize > 100) return branch('defect', 'VALIDATION');
    let credentials: Credentials;
    try { credentials = await this.client.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return branch('defect', 'AUTHENTICATION'); }
    const clientId = credentials.clientId instanceof SecretString ? credentials.clientId.reveal() : '';
    const clientSecret = credentials.clientSecret instanceof SecretString ? credentials.clientSecret.reveal() : '';
    if (!validCredential(clientId) || !validCredential(clientSecret)) return branch('defect', 'AUTHENTICATION');
    const tokenBody = new TextEncoder().encode(JSON.stringify({ client_id: clientId, client_secret: clientSecret }));
    const tokenResponse = await this.request(call, { url: tokenEndpoint, method: 'POST', headers: { 'Content-Type': 'application/json', Accept: 'application/json' }, body: tokenBody });
    if ('attempt' in tokenResponse) return tokenResponse.attempt;
    let tokenDocument: unknown;
    try { tokenDocument = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(tokenResponse.response.body)) as unknown; }
    catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(tokenDocument) || !validCredential(tokenDocument.tenant_access_token) || !Number.isSafeInteger(tokenDocument.expires_in) || (tokenDocument.expires_in as number) <= 0) return branch('invalidResponse', 'PROTOCOL');
    const query = new URLSearchParams({ access_token: tokenDocument.tenant_access_token, page_index: String(input.pageIndex), page_size: String(input.pageSize) });
    const projectResponse = await this.request(call, { url: projectsEndpoint + '?' + query.toString(), method: 'GET', headers: { Accept: 'application/json' } });
    if ('attempt' in projectResponse) return projectResponse.attempt;
    let document: unknown;
    try { document = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(projectResponse.response.body)) as unknown; }
    catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(document) || document.page_index !== input.pageIndex || !Number.isSafeInteger(document.page_size) || (document.page_size as number) < 1 || (document.page_size as number) > 100 || typeof document.has_more !== 'boolean' || !Array.isArray(document.items)) return branch('invalidResponse', 'PROTOCOL');
    const projects: Project[] = [];
    for (const item of document.items as unknown[]) {
      if (!isRecord(item) || typeof item._id !== 'string' || !item._id || typeof item.name !== 'string' || !item.name || (item.visibility !== 1 && item.visibility !== 2)) return branch('invalidResponse', 'PROTOCOL');
      projects.push({ id: item._id, name: item.name, identifier: typeof item.identifier === 'string' ? item.identifier : '', visibility: item.visibility });
    }
    return newQueryBranch('found', { pageIndex: input.pageIndex, pageSize: document.page_size as number, hasMore: document.has_more, projects });
  }
  private async request(call: Call, fields: Pick<ProviderRequest, 'url' | 'method' | 'headers' | 'body'>): Promise<{ response: ProviderResponse } | { attempt: QueryAttempt<ListProjectsOutput> }> {
    let response: ProviderResponse;
    try { response = await this.client.send({ ...fields, timeoutMs: this.client.timeoutMs, maxResponseBytes: this.client.maxResponseBytes,
      addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal }); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status === 200) return { attempt: branch('invalidResponse', 'RESPONSE_TOO_LARGE') };
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { attempt: classifyStatus(error.status, error.headers ?? {}) };
      return { attempt: retry('AVAILABILITY') };
    }
    if (response.status !== 200) return { attempt: classifyStatus(response.status, response.headers) };
    if (response.body.length > this.client.maxResponseBytes) return { attempt: branch('invalidResponse', 'RESPONSE_TOO_LARGE') };
    return { response };
  }
}
function validCredential(value: unknown): value is string { return typeof value === 'string' && value.length > 0 && value.length <= 1024 && /^[!-~]+$/.test(value); }
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function classifyStatus(status: number, headers: ProviderResponse['headers']): QueryAttempt<ListProjectsOutput> {
  const value = headers['retry-after']; const delay = parseRetryAfter(Array.isArray(value) ? value[0] ?? '' : value ?? '');
  if (status === 429) return retry('RATE_LIMIT', delay);
  if (status === 408 || status >= 500) return retry('AVAILABILITY', delay);
  return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
}
function failure(kind: Failure['kind']): Failure { return { provider: 'worktile', operation: 'listProjects', kind, message: 'Worktile project request failed' }; }
function branch(id: string, kind: Failure['kind']): QueryAttempt<ListProjectsOutput> { return newQueryBranch(id, emptyOutput, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<ListProjectsOutput> { return newQueryRetry(failure(kind), delay); }
