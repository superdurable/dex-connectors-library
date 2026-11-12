// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetContactDefinition, ListContactsDefinition } from './generated/definitions.js';
import type { Config, ContactMetadata, ContactsPage, Credentials, GetContactInput, ListContactsInput } from './generated/models.js';

const numericId = /^[0-9]{1,24}$/;
const countString = /^\+?[0-9]+$/;
const emptyContacts = (): ContactsPage => ({ contacts: [], offset: 0, limit: 0, total: 0 });
const emptyContact = (): ContactMetadata => ({ id: '', email: '', firstName: '', lastName: '' });

/** Optional transport dependency for provider tests and local Dex examples. */
export interface ActiveCampaignClientOptions {
  /** One bounded provider request sender. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

interface ProviderOutcome {
  body?: Uint8Array;
  branch?: string;
  failure?: Failure;
  retry?: boolean;
  retryAfterMs?: number;
}

/** Authenticated ActiveCampaign API client with call-time API Key resolution. */
export class ActiveCampaignClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly sendRequest: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate account configuration without contacting ActiveCampaign. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: ActiveCampaignClientOptions = {}) {
    if (!credentials) throw new TypeError('ActiveCampaign credential source is required');
    this.baseUrl = validateBaseURL(config.baseUrl);
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0) throw new TypeError('ActiveCampaign timeout is invalid');
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('ActiveCampaign response limit is invalid');
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
      headers: { Accept: 'application/json', 'Api-Token': apiKey },
      maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
      addressPolicy: this.baseUrl.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP',
      signal: call.context.cancellationSignal,
    };
    let response: ProviderResponse;
    try { response = await this.sendRequest(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) {
        response = { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() };
        if (response.status === 200) return { branch: 'invalidResponse', failure: failure(operation, 'RESPONSE_TOO_LARGE', 'ActiveCampaign response exceeded the configured limit') };
      } else return { retry: true, failure: failure(operation, 'TRANSPORT', 'ActiveCampaign request failed') };
    }
    const status = response.status;
    const retryAfter = headerValue(response.headers['retry-after']);
    if (status === 429) return { retry: true, retryAfterMs: parseRetryAfter(retryAfter), failure: failure(operation, 'RATE_LIMIT', 'ActiveCampaign rate limited the request') };
    if (status === 408 || status >= 500) return { retry: true, retryAfterMs: parseRetryAfter(retryAfter), failure: failure(operation, 'AVAILABILITY', 'ActiveCampaign is temporarily unavailable') };
    if (status === 404 && hasNotFound) return { branch: 'notFound', failure: failure(operation, 'NOT_FOUND', 'ActiveCampaign did not find the resource') };
    if (status !== 200) return { branch: 'providerRejected', failure: failure(operation, 'PROVIDER_REJECTION', `ActiveCampaign rejected HTTP ${status}`) };
    if (response.body.length > this.maxResponseBytes) return { branch: 'invalidResponse', failure: failure(operation, 'RESPONSE_TOO_LARGE', 'ActiveCampaign response exceeded the configured limit') };
    return { body: response.body };
  }
}

/** List one offset-based page of contacts accessible to the API user. */
export class ListContactsOperation implements Query<ListContactsInput, ContactsPage> {
  /** Bind the provider client. */ constructor(private readonly client: ActiveCampaignClient) {}
  /** Return the generated operation definition. */ definition(): QueryDefinition { return ListContactsDefinition; }
  /** Validate pagination and read contact metadata. */
  async invoke(call: Call, input: ListContactsInput): Promise<QueryAttempt<ContactsPage>> {
    const offset = input.offset ?? 0;
    const limit = input.limit === undefined || input.limit === 0 ? 20 : input.limit;
    if (!isNonnegativeInteger(offset) || !isPositiveInteger(limit) || limit > 100) return terminal('listContacts', 'defect', 'VALIDATION', 'offset or limit is invalid', emptyContacts());
    const result = await this.client.exchange(call, 'listContacts', `/contacts?limit=${limit}&offset=${offset}`, false);
    if (result.retry) return newQueryRetry(result.failure!, result.retryAfterMs);
    if (result.failure) return newQueryBranch(result.branch!, emptyContacts(), {}, result.failure);
    const document = parseJSON(result.body!);
    if (!isObject(document) || !Array.isArray(document.contacts) || !isObject(document.meta) || typeof document.meta.total !== 'string' || !countString.test(document.meta.total)) return invalid('listContacts', emptyContacts());
    const total = Number(document.meta.total);
    if (!Number.isSafeInteger(total) || total < 0) return invalid('listContacts', emptyContacts());
    const contacts: ContactMetadata[] = [];
    for (const item of document.contacts) {
      if (!isContact(item)) return invalid('listContacts', emptyContacts());
      contacts.push(contactMetadata(item));
    }
    return newQueryBranch('listed', { contacts, offset, limit, total });
  }
}

/** Read one contact without embedded relationship records. */
export class GetContactOperation implements Query<GetContactInput, ContactMetadata> {
  /** Bind the provider client. */ constructor(private readonly client: ActiveCampaignClient) {}
  /** Return the generated operation definition. */ definition(): QueryDefinition { return GetContactDefinition; }
  /** Validate the contact ID and read its metadata. */
  async invoke(call: Call, input: GetContactInput): Promise<QueryAttempt<ContactMetadata>> {
    if (!isNumericId(input.contactId)) return terminal('getContact', 'defect', 'VALIDATION', 'contact ID must be numeric', emptyContact());
    const result = await this.client.exchange(call, 'getContact', `/contacts/${input.contactId}`, true);
    if (result.retry) return newQueryRetry(result.failure!, result.retryAfterMs);
    if (result.failure) return newQueryBranch(result.branch!, emptyContact(), {}, result.failure);
    const document = parseJSON(result.body!);
    if (!isObject(document) || !isContact(document.contact)) return invalid('getContact', emptyContact());
    return newQueryBranch('found', contactMetadata(document.contact));
  }
}

function failure(operation: string, kind: Failure['kind'], message: string): Failure { return { provider: 'activecampaign', operation, kind, message }; }
function terminal<Output>(operation: string, branch: string, kind: Failure['kind'], message: string, empty: Output): QueryAttempt<Output> { return newQueryBranch(branch, empty, {}, failure(operation, kind, message)); }
function invalid<Output>(operation: string, empty: Output): QueryAttempt<Output> { return terminal(operation, 'invalidResponse', 'PROTOCOL', 'ActiveCampaign returned invalid JSON', empty); }
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function isNumericId(value: unknown): value is string { return typeof value === 'string' && numericId.test(value); }
function isPositiveInteger(value: unknown): value is number { return typeof value === 'number' && Number.isSafeInteger(value) && value > 0; }
function isNonnegativeInteger(value: unknown): value is number { return typeof value === 'number' && Number.isSafeInteger(value) && value >= 0; }
function isContact(value: unknown): value is Record<string, string> { return isObject(value) && isNumericId(value.id) && ['email', 'firstName', 'lastName'].every(field => value[field] === undefined || typeof value[field] === 'string'); }
function contactMetadata(value: Record<string, string>): ContactMetadata { return { id: value.id ?? '', email: value.email ?? '', firstName: value.firstName ?? '', lastName: value.lastName ?? '' }; }
function parseJSON(body: Uint8Array): unknown { try { return JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { return undefined; } }
function headerValue(value: string | string[] | undefined): string { return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
