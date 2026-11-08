// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetSubscriberDefinition, ListAccountsDefinition } from './generated/definitions.js';
import type { AccountMetadata, AccountsList, Config, Credentials, GetSubscriberInput, ListAccountsInput, SubscriberMetadata } from './generated/models.js';

const emptyAccounts = (): AccountsList => ({ accounts: [] });
const emptySubscriber = (): SubscriberMetadata => ({ id: '', email: '', firstName: '', lastName: '', status: '' });
const userAgent = 'Dex Connector (https://superdurable.io)';

/** Optional transport dependency for provider tests and local Dex examples. */
export interface DripClientOptions {
  /** One bounded provider request sender. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

interface ProviderOutcome {
  body?: Uint8Array;
  branch?: string;
  failure?: Failure;
  retry?: boolean;
  retryAfterMs?: number;
}

/** Authenticated Drip API v2 client with call-time API token resolution. */
export class DripClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly sendRequest: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate account configuration without contacting Drip. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: DripClientOptions = {}) {
    if (!credentials) throw new TypeError('Drip credential source is required');
    this.baseUrl = validateBaseURL(config.baseUrl || 'https://api.getdrip.com/v2');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0) throw new TypeError('Drip timeout is invalid');
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('Drip response limit is invalid');
    this.sendRequest = options.send ?? sendProviderRequest;
  }

  /** Bind the account discovery Query. */ listAccounts(): ListAccountsOperation { return new ListAccountsOperation(this); }
  /** Bind the subscriber metadata Query. */ getSubscriber(): GetSubscriberOperation { return new GetSubscriberOperation(this); }

  /** Send one read and classify transport, HTTP, and response-size outcomes. */
  async exchange(call: Call, operation: string, path: string, hasNotFound: boolean): Promise<ProviderOutcome> {
    let apiKey: string;
    try {
      const resolved = await this.credentials.resolve(call, call.context.cancellationSignal);
      apiKey = resolved.apiKey instanceof SecretString ? resolved.apiKey.reveal() : '';
    } catch { return { branch: 'defect', failure: failure(operation, 'AUTHENTICATION', 'connection credentials are unavailable or invalid') }; }
    if (!apiKey || !isHeaderSafeCredential(apiKey)) return { branch: 'defect', failure: failure(operation, 'AUTHENTICATION', 'connection credentials are unavailable or invalid') };
    const request: ProviderRequest = {
      url: `${this.baseUrl}${path}`, method: 'GET',
      headers: { Accept: 'application/json', Authorization: `Basic ${Buffer.from(`${apiKey}:`, 'utf8').toString('base64')}`, 'User-Agent': userAgent },
      maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
      addressPolicy: this.baseUrl.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP',
      signal: call.context.cancellationSignal,
    };
    let response: ProviderResponse;
    try { response = await this.sendRequest(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) {
        response = { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() };
        if (response.status === 200) return { branch: 'invalidResponse', failure: failure(operation, 'RESPONSE_TOO_LARGE', 'Drip response exceeded the configured limit') };
      } else return { retry: true, failure: failure(operation, 'TRANSPORT', 'Drip request failed') };
    }
    const status = response.status;
    const retryAfter = parseRetryAfter(headerValue(response.headers['retry-after']));
    if (status === 429) return { retry: true, retryAfterMs: retryAfter, failure: failure(operation, 'RATE_LIMIT', 'Drip rate limited the request') };
    if (status === 408 || status >= 500) return { retry: true, retryAfterMs: retryAfter, failure: failure(operation, 'AVAILABILITY', 'Drip is temporarily unavailable') };
    if (status === 404 && hasNotFound) return { branch: 'notFound', failure: failure(operation, 'NOT_FOUND', 'Drip did not find the resource') };
    if (status !== 200) return { branch: 'providerRejected', failure: failure(operation, 'PROVIDER_REJECTION', `Drip rejected HTTP ${status}`) };
    if (response.body.length > this.maxResponseBytes) return { branch: 'invalidResponse', failure: failure(operation, 'RESPONSE_TOO_LARGE', 'Drip response exceeded the configured limit') };
    return { body: response.body };
  }
}

/** List Drip account IDs and names without private settings. */
export class ListAccountsOperation implements Query<ListAccountsInput, AccountsList> {
  /** Bind the provider client. */ constructor(private readonly client: DripClient) {}
  /** Return the manifest-generated operation definition. */ definition(): QueryDefinition { return ListAccountsDefinition; }
  /** Read accessible account identities. */
  async invoke(call: Call, input: ListAccountsInput): Promise<QueryAttempt<AccountsList>> {
    if (!input || typeof input !== 'object') return terminal('listAccounts', 'defect', 'VALIDATION', 'account list input is invalid', emptyAccounts());
    const result = await this.client.exchange(call, 'listAccounts', '/accounts', false);
    if (result.retry) return newQueryRetry(result.failure!, result.retryAfterMs);
    if (result.failure) return newQueryBranch(result.branch!, emptyAccounts(), {}, result.failure);
    const document = parseJSON(result.body!);
    if (!isObject(document) || !Array.isArray(document.accounts)) return invalid('listAccounts', emptyAccounts());
    const accounts: AccountMetadata[] = [];
    for (const item of document.accounts) {
      if (!isObject(item) || typeof item.id !== 'string' || !item.id || typeof item.name !== 'string' || !item.name) return invalid('listAccounts', emptyAccounts());
      accounts.push({ id: item.id, name: item.name });
    }
    return newQueryBranch('listed', { accounts });
  }
}

/** Read one subscriber's identity and status without custom fields. */
export class GetSubscriberOperation implements Query<GetSubscriberInput, SubscriberMetadata> {
  /** Bind the provider client. */ constructor(private readonly client: DripClient) {}
  /** Return the manifest-generated operation definition. */ definition(): QueryDefinition { return GetSubscriberDefinition; }
  /** Validate identifiers and read the selected subscriber. */
  async invoke(call: Call, input: GetSubscriberInput): Promise<QueryAttempt<SubscriberMetadata>> {
    if (!isValidInput(input)) return terminal('getSubscriber', 'defect', 'VALIDATION', 'account ID or subscriber identifier is invalid', emptySubscriber());
    const path = `/${input.accountId}/subscribers/${encodeURIComponent(input.subscriberIdOrEmail)}`;
    const result = await this.client.exchange(call, 'getSubscriber', path, true);
    if (result.retry) return newQueryRetry(result.failure!, result.retryAfterMs);
    if (result.failure) return newQueryBranch(result.branch!, emptySubscriber(), {}, result.failure);
    const document = parseJSON(result.body!);
    if (!isObject(document) || !Array.isArray(document.subscribers) || document.subscribers.length !== 1) return invalid('getSubscriber', emptySubscriber());
    const item = document.subscribers[0];
    if (!isSubscriber(item)) return invalid('getSubscriber', emptySubscriber());
    return newQueryBranch('found', { id: item.id as string, email: item.email as string, firstName: (item.first_name as string | null | undefined) ?? '', lastName: (item.last_name as string | null | undefined) ?? '', status: item.status as string });
  }
}

function isValidInput(input: GetSubscriberInput): boolean {
  if (!input || typeof input.accountId !== 'string' || !/^[0-9]+$/.test(input.accountId) || input.accountId.length > 20 || BigInt(input.accountId) === 0n || BigInt(input.accountId) > 18446744073709551615n || typeof input.subscriberIdOrEmail !== 'string' || !input.subscriberIdOrEmail || input.subscriberIdOrEmail.trim() !== input.subscriberIdOrEmail || Buffer.byteLength(input.subscriberIdOrEmail, 'utf8') > 512) return false;
  try { encodeURIComponent(input.subscriberIdOrEmail); return true; } catch { return false; }
}
function isSubscriber(value: unknown): value is Record<string, unknown> { return isObject(value) && typeof value.id === 'string' && !!value.id && typeof value.email === 'string' && !!value.email && typeof value.status === 'string' && !!value.status && (value.first_name === undefined || value.first_name === null || typeof value.first_name === 'string') && (value.last_name === undefined || value.last_name === null || typeof value.last_name === 'string'); }
function failure(operation: string, kind: Failure['kind'], message: string): Failure { return { provider: 'drip', operation, kind, message }; }
function terminal<Output>(operation: string, branch: string, kind: Failure['kind'], message: string, empty: Output): QueryAttempt<Output> { return newQueryBranch(branch, empty, {}, failure(operation, kind, message)); }
function invalid<Output>(operation: string, empty: Output): QueryAttempt<Output> { return terminal(operation, 'invalidResponse', 'PROTOCOL', 'Drip returned invalid JSON', empty); }
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function parseJSON(body: Uint8Array): unknown { try { return JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { return undefined; } }
function headerValue(value: string | string[] | undefined): string { return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
