// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, ReauthorizationRequiredError, resolveCredential, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type CredentialRefreshDriver, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { ManifestCredentialRefreshDriver } from '@superdurable/connectors-sdk/oauthtoken';
import { BodyTooLargeError, ProviderTransportError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListAccountsDefinition, ListContainersDefinition, ListTagsDefinition } from './generated/definitions.js';
import { Authorization, CredentialFields } from './generated/refresh.js';
import type { AccountsPage, Config, ContainersPage, Credentials, ListAccountsInput, ListContainersInput, ListTagsInput, TagManagerAccount, TagManagerContainer, TagManagerTag, TagsPage } from './generated/models.js';

/** Injectable provider transport and OAuth refresh dependencies. */
export interface TagManagerClientOptions {
  /** Provider sender; omitted uses the SDK HTTP transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Refresh driver; omitted uses the manifest declaration. */ refreshDriver?: CredentialRefreshDriver<Credentials>;
  /** Clock used to interpret HTTP Retry-After dates. */ now?: () => Date;
}

/** One bounded Tag Manager response. */
export interface TagManagerExchange {
  /** Provider status, or zero before response headers. */ status: number;
  /** Bounded response bytes retained only in process memory. */ body: Uint8Array;
  /** Retry-After delay in milliseconds. */ retryAfterMs: number;
  /** Safe local error class. */ error?: 'credential' | 'refresh' | 'request' | 'tooLarge';
}

/** Tag Manager client that resolves credentials for each call. */
export class TagManagerClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly refreshDriver: CredentialRefreshDriver<Credentials>;
  private readonly sendRequest: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;

  /** Validate configuration without contacting Google. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: TagManagerClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Google Tag Manager configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl || 'https://tagmanager.googleapis.com');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 2_097_152;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('Google Tag Manager request limits are invalid');
    this.refreshDriver = options.refreshDriver ?? new ManifestCredentialRefreshDriver<Credentials>({
      providerName: 'Google Tag Manager', auth: Authorization, credentialFields: CredentialFields,
    });
    this.sendRequest = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the account Query. */ listAccounts(): ListAccountsOperation { return new ListAccountsOperation(this); }
  /** Bind the container Query. */ listContainers(): ListContainersOperation { return new ListContainersOperation(this); }
  /** Bind the tag Query. */ listTags(): ListTagsOperation { return new ListTagsOperation(this); }

  /** Send one bounded provider GET for a Query. */
  async request(call: Call, path: string): Promise<TagManagerExchange> {
    let token: string;
    try {
      const resolved = await resolveCredential(this.credentials, call, this.refreshDriver, call.context.cancellationSignal);
      token = resolved.accessToken instanceof SecretString ? resolved.accessToken.reveal() : '';
      if (!token || !isHeaderSafeCredential(token)) return failedExchange('credential');
    } catch (error) {
      return failedExchange(error instanceof ReauthorizationRequiredError ? 'credential' : 'refresh');
    }
    const request: ProviderRequest = {
      url: this.baseURL + path, method: 'GET',
      headers: { Accept: 'application/json', Authorization: `Bearer ${token}`, 'User-Agent': 'Dex Connector (https://superdurable.io)' },
      maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
      addressPolicy: this.baseURL.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP',
      signal: call.context.cancellationSignal,
    };
    let response: ProviderResponse;
    try { response = await this.sendRequest(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { ...failedExchange('tooLarge'), status: error.status, retryAfterMs: parseRetryAfter(header(error.headers ?? {}, 'retry-after'), this.now()) };
      if (error instanceof ProviderTransportError) return failedExchange('request');
      return failedExchange('request');
    }
    const retryAfterMs = parseRetryAfter(header(response.headers, 'retry-after'), this.now());
    if (Buffer.from(response.body).includes(token)) return { ...failedExchange('credential'), status: response.status, retryAfterMs };
    return { status: response.status, body: response.body, retryAfterMs };
  }
}

/** List accounts visible to the connected Google user. */
export class ListAccountsOperation implements Query<ListAccountsInput, AccountsPage> {
  /** Bind the provider client. */ constructor(private readonly client: TagManagerClient) {}
  /** Return the generated account Query definition. */ definition(): QueryDefinition { return ListAccountsDefinition; }
  /** Project one bounded account page. */
  async invoke(call: Call, input: ListAccountsInput): Promise<QueryAttempt<AccountsPage>> {
    const operation = 'listAccounts'; const empty: AccountsPage = { accounts: [], nextPageToken: '' };
    if (!input || !validPageToken(input.pageToken)) return queryBranch(operation, 'defect', empty, 'VALIDATION', 'account page token is invalid');
    const result = await readPage(this.client, call, operation, pagePath('/tagmanager/v2/accounts', input.pageToken), 'account', 'accounts', empty);
    if ('attempt' in result) return result.attempt;
    const accounts: TagManagerAccount[] = [];
    for (const entry of result.entries) {
      if (!isObject(entry) || !validResource(entry.path, 'accounts') || typeof entry.name !== 'string' || !entry.name)
        return queryBranch(operation, 'invalidResponse', empty, 'PROTOCOL', 'Google returned incomplete account metadata');
      accounts.push({ path: entry.path, name: entry.name });
    }
    return newQueryBranch('listed', { accounts, nextPageToken: result.nextPageToken });
  }
}

/** List containers in one Tag Manager account. */
export class ListContainersOperation implements Query<ListContainersInput, ContainersPage> {
  /** Bind the provider client. */ constructor(private readonly client: TagManagerClient) {}
  /** Return the generated container Query definition. */ definition(): QueryDefinition { return ListContainersDefinition; }
  /** Project one bounded container page. */
  async invoke(call: Call, input: ListContainersInput): Promise<QueryAttempt<ContainersPage>> {
    const operation = 'listContainers'; const empty: ContainersPage = { containers: [], nextPageToken: '' };
    if (!input || !validID(input.accountId) || !validPageToken(input.pageToken)) return queryBranch(operation, 'defect', empty, 'VALIDATION', 'container page input is invalid');
    const prefix = `accounts/${input.accountId}/containers`;
    const result = await readPage(this.client, call, operation, pagePath(`/tagmanager/v2/${prefix}`, input.pageToken), 'container', 'container', empty);
    if ('attempt' in result) return result.attempt;
    const containers: TagManagerContainer[] = [];
    for (const entry of result.entries) {
      if (!isObject(entry) || !validResource(entry.path, prefix) || typeof entry.name !== 'string' || !entry.name || typeof entry.publicId !== 'string' || !entry.publicId)
        return queryBranch(operation, 'invalidResponse', empty, 'PROTOCOL', 'Google returned incomplete container metadata');
      containers.push({ path: entry.path, name: entry.name, publicId: entry.publicId });
    }
    return newQueryBranch('listed', { containers, nextPageToken: result.nextPageToken });
  }
}

/** List tag identities in one Tag Manager workspace. */
export class ListTagsOperation implements Query<ListTagsInput, TagsPage> {
  /** Bind the provider client. */ constructor(private readonly client: TagManagerClient) {}
  /** Return the generated tag Query definition. */ definition(): QueryDefinition { return ListTagsDefinition; }
  /** Project one bounded tag page. */
  async invoke(call: Call, input: ListTagsInput): Promise<QueryAttempt<TagsPage>> {
    const operation = 'listTags'; const empty: TagsPage = { tags: [], nextPageToken: '' };
    if (!input || !validID(input.accountId) || !validID(input.containerId) || !validID(input.workspaceId) || !validPageToken(input.pageToken))
      return queryBranch(operation, 'defect', empty, 'VALIDATION', 'tag page input is invalid');
    const prefix = `accounts/${input.accountId}/containers/${input.containerId}/workspaces/${input.workspaceId}/tags`;
    const result = await readPage(this.client, call, operation, pagePath(`/tagmanager/v2/${prefix}`, input.pageToken), 'tag', 'tag', empty);
    if ('attempt' in result) return result.attempt;
    const tags: TagManagerTag[] = [];
    for (const entry of result.entries) {
      if (!isObject(entry) || !validResource(entry.path, prefix) || typeof entry.name !== 'string' || !entry.name || typeof entry.type !== 'string' || !entry.type)
        return queryBranch(operation, 'invalidResponse', empty, 'PROTOCOL', 'Google returned incomplete tag metadata');
      tags.push({ path: entry.path, name: entry.name, type: entry.type });
    }
    return newQueryBranch('listed', { tags, nextPageToken: result.nextPageToken });
  }
}

async function readPage<Output>(client: TagManagerClient, call: Call, operation: string, path: string, field: string, resource: string, empty: Output): Promise<{ entries: unknown[]; nextPageToken: string } | { attempt: QueryAttempt<Output> }> {
  const exchange = await client.request(call, path);
  const failure = classifyRead(operation, exchange, empty);
  if (failure) return { attempt: failure };
  const document = parseJSON(exchange.body);
  const entries = isObject(document) ? document[field] ?? [] : undefined;
  const nextPageToken = isObject(document) ? document.nextPageToken ?? '' : undefined;
  if (!Array.isArray(entries) || entries.length > 1000 || !validPageToken(nextPageToken))
    return { attempt: queryBranch(operation, 'invalidResponse', empty, 'PROTOCOL', `Google returned invalid ${resource} JSON`) };
  return { entries, nextPageToken };
}

function classifyRead<Output>(operation: string, exchange: TagManagerExchange, empty: Output): QueryAttempt<Output> | undefined {
  if (exchange.error === 'credential') return queryBranch(operation, 'defect', empty, 'AUTHENTICATION', 'connection credentials are unavailable or invalid');
  if (exchange.error === 'request' || exchange.error === 'refresh') return queryRetry(operation, 'TRANSPORT', 'Google Tag Manager request failed');
  if (exchange.status === 429) return queryRetry(operation, 'RATE_LIMIT', 'Google rate limited the request', exchange.retryAfterMs);
  if (exchange.status === 408 || exchange.status >= 500) return queryRetry(operation, 'AVAILABILITY', 'Google Tag Manager is temporarily unavailable', exchange.retryAfterMs);
  if (exchange.status !== 200) return queryBranch(operation, 'providerRejected', empty, 'PROVIDER_REJECTION', `Google rejected HTTP ${exchange.status}`);
  if (exchange.error === 'tooLarge') return queryBranch(operation, 'invalidResponse', empty, 'RESPONSE_TOO_LARGE', 'Google response exceeded the configured limit');
  return undefined;
}
function queryBranch<Output>(operation: string, branch: string, empty: Output, kind: Failure['kind'], message: string): QueryAttempt<Output> { return newQueryBranch(branch, empty, {}, { kind, provider: 'google-tag-manager', operation, message }); }
function queryRetry<Output>(operation: string, kind: Failure['kind'], message: string, delay = 0): QueryAttempt<Output> { return newQueryRetry({ kind, provider: 'google-tag-manager', operation, message }, delay); }
function validID(value: unknown): value is string { return typeof value === 'string' && /^[0-9]{1,30}$/.test(value); }
function validPageToken(value: unknown): value is string { return typeof value === 'string' && Buffer.byteLength(value) <= 4096 && !/[\r\n]/.test(value); }
function validResource(value: unknown, prefix: string): value is string { return typeof value === 'string' && value.startsWith(`${prefix}/`) && validID(value.slice(prefix.length + 1)); }
function pagePath(path: string, token: string): string { return path + (token ? `?${new URLSearchParams({ pageToken: token })}` : ''); }
function failedExchange(error: NonNullable<TagManagerExchange['error']>): TagManagerExchange { return { status: 0, body: new Uint8Array(), retryAfterMs: 0, error }; }
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function parseJSON(body: Uint8Array): unknown { try { return JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { return undefined; } }
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name.toLowerCase())?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
