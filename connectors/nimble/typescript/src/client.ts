// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetContactDefinition, ListContactIDsDefinition } from './generated/definitions.js';
import type { Config, ContactIDsPage, ContactMetadata, Credentials, GetContactInput, ListContactIDsInput } from './generated/models.js';

const emptyPage = (): ContactIDsPage => ({ ids: [], page: 0, pages: 0, perPage: 0, total: 0 });
const emptyContact = (): ContactMetadata => ({ id: '', firstName: '', lastName: '', emailAddress: '' });
const userAgent = 'Dex Connector (https://superdurable.io)';

/** Optional transport dependency for provider tests and local Dex examples. */
export interface NimbleClientOptions {
  /** One bounded provider request sender. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

interface ProviderOutcome {
  body?: Uint8Array;
  branch?: string;
  failure?: Failure;
  retry?: boolean;
  retryAfterMs?: number;
}

/** Authenticated Nimble CRM API v1 client with call-time credential resolution. */
export class NimbleClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly sendRequest: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate connection configuration without contacting Nimble. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: NimbleClientOptions = {}) {
    if (!credentials) throw new TypeError('Nimble credential source is required');
    this.baseUrl = validateBaseURL(config.baseUrl || 'https://app.nimble.com/api/v1');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0) throw new TypeError('Nimble timeout is invalid');
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('Nimble response limit is invalid');
    this.sendRequest = options.send ?? sendProviderRequest;
  }

  /** Bind the contact page Query. */ listContactIDs(): ListContactIDsOperation { return new ListContactIDsOperation(this); }
  /** Bind the contact metadata Query. */ getContact(): GetContactOperation { return new GetContactOperation(this); }

  /** Send one read and classify transport, HTTP, and response-size outcomes. */
  async exchange(call: Call, operation: string, path: string, hasNotFound: boolean): Promise<ProviderOutcome> {
    let apiKey: string;
    try {
      const resolved = await this.credentials.resolve(call, call.context.cancellationSignal);
      apiKey = resolved.apiKey instanceof SecretString ? resolved.apiKey.reveal() : '';
    } catch { return { branch: 'defect', failure: failure(operation, 'AUTHENTICATION', 'connection credentials are unavailable or invalid') }; }
    if (!isHeaderSafeCredential(apiKey)) return { branch: 'defect', failure: failure(operation, 'AUTHENTICATION', 'connection credentials are unavailable or invalid') };
    const request: ProviderRequest = {
      url: `${this.baseUrl}${path}`, method: 'GET',
      headers: { Accept: 'application/json', Authorization: `Bearer ${apiKey}`, 'User-Agent': userAgent },
      maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
      addressPolicy: this.baseUrl.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP',
      signal: call.context.cancellationSignal,
    };
    let response: ProviderResponse;
    try { response = await this.sendRequest(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) {
        response = { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() };
        if (response.status === 200) return { branch: 'invalidResponse', failure: failure(operation, 'RESPONSE_TOO_LARGE', 'Nimble response exceeded the configured limit') };
      } else return { retry: true, failure: failure(operation, 'TRANSPORT', 'Nimble request failed') };
    }
    const status = response.status;
    const retryAfter = parseRetryAfter(headerValue(response.headers['retry-after']));
    if (status === 429) return { retry: true, retryAfterMs: retryAfter, failure: failure(operation, 'RATE_LIMIT', 'Nimble rate limited the request') };
    if (status === 408 || status >= 500) return { retry: true, retryAfterMs: retryAfter, failure: failure(operation, 'AVAILABILITY', 'Nimble is temporarily unavailable') };
    if (status === 404 && hasNotFound) return { branch: 'notFound', failure: failure(operation, 'NOT_FOUND', 'Nimble did not find the resource') };
    if (status !== 200) return { branch: 'providerRejected', failure: failure(operation, 'PROVIDER_REJECTION', `Nimble rejected HTTP ${status}`) };
    if (response.body.length > this.maxResponseBytes) return { branch: 'invalidResponse', failure: failure(operation, 'RESPONSE_TOO_LARGE', 'Nimble response exceeded the configured limit') };
    return { body: response.body };
  }
}

/** List Nimble contact IDs with provider pagination metadata. */
export class ListContactIDsOperation implements Query<ListContactIDsInput, ContactIDsPage> {
  /** Bind the provider client. */ constructor(private readonly client: NimbleClient) {}
  /** Return the manifest-generated operation definition. */ definition(): QueryDefinition { return ListContactIDsDefinition; }
  /** Read one bounded contact ID page. */
  async invoke(call: Call, input: ListContactIDsInput): Promise<QueryAttempt<ContactIDsPage>> {
    if (!input || !Number.isSafeInteger(input.page) || input.page < 1 || !Number.isSafeInteger(input.perPage) || input.perPage < 1 || input.perPage > 100) return terminal('listContactIDs', 'defect', 'VALIDATION', 'contact page number or size is invalid', emptyPage());
    const result = await this.client.exchange(call, 'listContactIDs', `/contacts/ids?page=${input.page}&per_page=${input.perPage}`, false);
    if (result.retry) return newQueryRetry(result.failure!, result.retryAfterMs);
    if (result.failure) return newQueryBranch(result.branch!, emptyPage(), {}, result.failure);
    const document = parseJSON(result.body!);
    if (!isObject(document) || !isObject(document.meta) || !Array.isArray(document.resources)) return invalid('listContactIDs', emptyPage());
    const meta = document.meta;
    if (!document.resources.every(isContactID) || !isNonnegativeSafeInteger(meta.pages) || !isNonnegativeSafeInteger(meta.total) || !isPositiveSafeInteger(meta.page) || !isPositiveSafeInteger(meta.per_page)) return invalid('listContactIDs', emptyPage());
    return newQueryBranch('listed', { ids: document.resources, page: meta.page, pages: meta.pages, perPage: meta.per_page, total: meta.total });
  }
}

/** Read one Nimble contact's identity without custom fields. */
export class GetContactOperation implements Query<GetContactInput, ContactMetadata> {
  /** Bind the provider client. */ constructor(private readonly client: NimbleClient) {}
  /** Return the manifest-generated operation definition. */ definition(): QueryDefinition { return GetContactDefinition; }
  /** Validate the contact ID and read its selected fields. */
  async invoke(call: Call, input: GetContactInput): Promise<QueryAttempt<ContactMetadata>> {
    if (!input || !isContactID(input.contactId)) return terminal('getContact', 'defect', 'VALIDATION', 'contact ID must be a 24-character hexadecimal identifier', emptyContact());
    const result = await this.client.exchange(call, 'getContact', `/contact/${input.contactId}`, true);
    if (result.retry) return newQueryRetry(result.failure!, result.retryAfterMs);
    if (result.failure) return newQueryBranch(result.branch!, emptyContact(), {}, result.failure);
    const document = parseJSON(result.body!);
    if (!isObject(document) || !Array.isArray(document.resources) || document.resources.length !== 1) return invalid('getContact', emptyContact());
    const contact = document.resources[0];
    if (!isObject(contact) || !isContactID(contact.id) || contact.id.toLowerCase() !== input.contactId.toLowerCase() || (contact.fields !== undefined && contact.fields !== null && !isObject(contact.fields))) return invalid('getContact', emptyContact());
    const fields = isObject(contact.fields) ? contact.fields : {};
    if (!Object.values(fields).every(validFieldValues)) return invalid('getContact', emptyContact());
    return newQueryBranch('found', { id: contact.id, firstName: firstFieldValue(fields['first name']), lastName: firstFieldValue(fields['last name']), emailAddress: firstFieldValue(fields.email) });
  }
}

function isContactID(value: unknown): value is string { return typeof value === 'string' && /^[0-9a-fA-F]{24}$/.test(value); }
function validFieldValues(value: unknown): boolean { return value === null || Array.isArray(value) && value.every(item => item === null || isObject(item) && (item.value === undefined || item.value === null || typeof item.value === 'string')); }
function firstFieldValue(value: unknown): string { if (!Array.isArray(value) || value.length === 0 || !isObject(value[0])) return ''; return typeof value[0].value === 'string' ? value[0].value : ''; }
function isPositiveSafeInteger(value: unknown): value is number { return typeof value === 'number' && Number.isSafeInteger(value) && value > 0; }
function isNonnegativeSafeInteger(value: unknown): value is number { return typeof value === 'number' && Number.isSafeInteger(value) && value >= 0; }
function failure(operation: string, kind: Failure['kind'], message: string): Failure { return { provider: 'nimble', operation, kind, message }; }
function terminal<Output>(operation: string, branch: string, kind: Failure['kind'], message: string, empty: Output): QueryAttempt<Output> { return newQueryBranch(branch, empty, {}, failure(operation, kind, message)); }
function invalid<Output>(operation: string, empty: Output): QueryAttempt<Output> { return terminal(operation, 'invalidResponse', 'PROTOCOL', 'Nimble returned invalid JSON', empty); }
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function parseJSON(body: Uint8Array): unknown { try { return JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { return undefined; } }
function headerValue(value: string | string[] | undefined): string { return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
