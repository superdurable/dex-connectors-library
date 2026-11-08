// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetContactDefinition, ListContactsDefinition } from './generated/definitions.js';
import type { Config, ContactMetadata, ContactsPage, Credentials, GetContactInput, ListContactsInput } from './generated/models.js';

const contactIdPattern = /^[A-Za-z0-9]{1,64}$/;
const emptyContacts = (): ContactsPage => ({ contacts: [], page: 0, perPage: 0 });
const emptyContact = (): ContactMetadata => ({ contactId: '', email: '', name: '' });

/** Optional transport dependency for provider tests and local Dex examples. */
export interface GetResponseClientOptions {
  /** One bounded provider request sender. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

interface ProviderOutcome {
  body?: Uint8Array;
  branch?: string;
  failure?: Failure;
  retry?: boolean;
  retryAfterMs?: number;
}

/** Authenticated GetResponse SMB API client with call-time API Key resolution. */
export class GetResponseClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly sendRequest: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate account configuration without contacting GetResponse. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: GetResponseClientOptions = {}) {
    if (!credentials) throw new TypeError('GetResponse credential source is required');
    this.baseUrl = validateBaseURL(config.baseUrl || 'https://api.getresponse.com/v3');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0) throw new TypeError('GetResponse timeout is invalid');
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('GetResponse response limit is invalid');
    this.sendRequest = options.send ?? sendProviderRequest;
  }

  /** Bind the contact page Query. */ listContacts(): ListContactsOperation { return new ListContactsOperation(this); }
  /** Bind the contact metadata Query. */ getContact(): GetContactOperation { return new GetContactOperation(this); }

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
      headers: { Accept: 'application/json', 'X-Auth-Token': `api-key ${apiKey}` },
      maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
      addressPolicy: this.baseUrl.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP',
      signal: call.context.cancellationSignal,
    };
    let response: ProviderResponse;
    try { response = await this.sendRequest(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) {
        response = { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() };
        if (response.status === 200) return { branch: 'invalidResponse', failure: failure(operation, 'RESPONSE_TOO_LARGE', 'GetResponse response exceeded the configured limit') };
      } else return { retry: true, failure: failure(operation, 'TRANSPORT', 'GetResponse request failed') };
    }
    const status = response.status;
    const retryAfter = parseRetryAfter(headerValue(response.headers['retry-after']));
    if (status === 429) {
      const reset = Number(headerValue(response.headers['x-ratelimit-reset'] ?? response.headers['X-RateLimit-Reset']));
      const delay = retryAfter || (Number.isSafeInteger(reset) && reset > 0 ? reset * 1000 : 0);
      return { retry: true, retryAfterMs: delay, failure: failure(operation, 'RATE_LIMIT', 'GetResponse rate limited the request') };
    }
    if (status === 408 || status >= 500) return { retry: true, retryAfterMs: retryAfter, failure: failure(operation, 'AVAILABILITY', 'GetResponse is temporarily unavailable') };
    if (status === 404 && hasNotFound) return { branch: 'notFound', failure: failure(operation, 'NOT_FOUND', 'GetResponse did not find the resource') };
    if (status !== 200) return { branch: 'providerRejected', failure: failure(operation, 'PROVIDER_REJECTION', `GetResponse rejected HTTP ${status}`) };
    if (response.body.length > this.maxResponseBytes) return { branch: 'invalidResponse', failure: failure(operation, 'RESPONSE_TOO_LARGE', 'GetResponse response exceeded the configured limit') };
    return { body: response.body };
  }
}

/** List one page of contacts accessible to the API user. */
export class ListContactsOperation implements Query<ListContactsInput, ContactsPage> {
  /** Bind the provider client. */ constructor(private readonly client: GetResponseClient) {}
  /** Return the generated operation definition. */ definition(): QueryDefinition { return ListContactsDefinition; }
  /** Validate pagination and read contact metadata. */
  async invoke(call: Call, input: ListContactsInput): Promise<QueryAttempt<ContactsPage>> {
    const page = input.page || 1;
    const perPage = input.perPage || 20;
    if (!isNonnegativeInteger(input.page ?? 0) || !isNonnegativeInteger(input.perPage ?? 0) || perPage > 1000) return terminal('listContacts', 'defect', 'VALIDATION', 'page or perPage is invalid', emptyContacts());
    const result = await this.client.exchange(call, 'listContacts', `/contacts?page=${page}&perPage=${perPage}`, false);
    if (result.retry) return newQueryRetry(result.failure!, result.retryAfterMs);
    if (result.failure) return newQueryBranch(result.branch!, emptyContacts(), {}, result.failure);
    const document = parseJSON(result.body!);
    if (!Array.isArray(document)) return invalid('listContacts', emptyContacts());
    const contacts: ContactMetadata[] = [];
    for (const item of document) {
      if (!isContact(item)) return invalid('listContacts', emptyContacts());
      contacts.push(contactMetadata(item));
    }
    return newQueryBranch('listed', { contacts, page, perPage });
  }
}

/** Read one contact without embedded custom fields. */
export class GetContactOperation implements Query<GetContactInput, ContactMetadata> {
  /** Bind the provider client. */ constructor(private readonly client: GetResponseClient) {}
  /** Return the generated operation definition. */ definition(): QueryDefinition { return GetContactDefinition; }
  /** Validate the contact ID and read its metadata. */
  async invoke(call: Call, input: GetContactInput): Promise<QueryAttempt<ContactMetadata>> {
    if (!isContactId(input.contactId)) return terminal('getContact', 'defect', 'VALIDATION', 'contact ID must be alphanumeric', emptyContact());
    const result = await this.client.exchange(call, 'getContact', `/contacts/${input.contactId}`, true);
    if (result.retry) return newQueryRetry(result.failure!, result.retryAfterMs);
    if (result.failure) return newQueryBranch(result.branch!, emptyContact(), {}, result.failure);
    const document = parseJSON(result.body!);
    if (!isContact(document)) return invalid('getContact', emptyContact());
    return newQueryBranch('found', contactMetadata(document));
  }
}

function failure(operation: string, kind: Failure['kind'], message: string): Failure { return { provider: 'getresponse', operation, kind, message }; }
function terminal<Output>(operation: string, branch: string, kind: Failure['kind'], message: string, empty: Output): QueryAttempt<Output> { return newQueryBranch(branch, empty, {}, failure(operation, kind, message)); }
function invalid<Output>(operation: string, empty: Output): QueryAttempt<Output> { return terminal(operation, 'invalidResponse', 'PROTOCOL', 'GetResponse returned invalid JSON', empty); }
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function isContactId(value: unknown): value is string { return typeof value === 'string' && contactIdPattern.test(value); }
function isNonnegativeInteger(value: unknown): value is number { return typeof value === 'number' && Number.isSafeInteger(value) && value >= 0; }
function isContact(value: unknown): value is Record<string, string | null> { return isObject(value) && isContactId(value.contactId) && typeof value.email === 'string' && value.email.length > 0 && (value.name === undefined || value.name === null || typeof value.name === 'string'); }
function contactMetadata(value: Record<string, string | null>): ContactMetadata { return { contactId: value.contactId ?? '', email: value.email ?? '', name: value.name ?? '' }; }
function parseJSON(body: Uint8Array): unknown { try { return JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { return undefined; } }
function headerValue(value: string | string[] | undefined): string { return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
