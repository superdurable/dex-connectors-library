// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetContactDefinition, ListContactsDefinition } from './generated/definitions.js';
import type { Config, ContactMetadata, ContactsPage, Credentials, GetContactInput, ListContactsInput } from './generated/models.js';

const emptyPage = (): ContactsPage => ({ contacts: [] });
const emptyContact = (): ContactMetadata => ({ id: 0, firstName: '', lastName: '', emailAddress: '' });
const userAgent = 'Dex Connector (https://superdurable.io)';

/** Optional transport dependency for provider tests and local Dex examples. */
export interface InsightlyClientOptions {
  /** One bounded provider request sender. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

interface ProviderOutcome {
  body?: Uint8Array;
  branch?: string;
  failure?: Failure;
  retry?: boolean;
  retryAfterMs?: number;
}

/** Authenticated Insightly CRM API v3.1 client with call-time credential resolution. */
export class InsightlyClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly sendRequest: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate connection configuration without contacting Insightly. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: InsightlyClientOptions = {}) {
    if (!credentials) throw new TypeError('Insightly credential source is required');
    this.baseUrl = validateBaseURL(config.baseUrl || 'https://api.na1.insightly.com/v3.1');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0) throw new TypeError('Insightly timeout is invalid');
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('Insightly response limit is invalid');
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
    if (!isHeaderSafeCredential(apiKey)) return { branch: 'defect', failure: failure(operation, 'AUTHENTICATION', 'connection credentials are unavailable or invalid') };
    const request: ProviderRequest = {
      url: `${this.baseUrl}${path}`, method: 'GET',
      headers: { Accept: 'application/json', Authorization: `Basic ${Buffer.from(`${apiKey}:`).toString('base64')}`, 'User-Agent': userAgent },
      maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
      addressPolicy: this.baseUrl.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP',
      signal: call.context.cancellationSignal,
    };
    let response: ProviderResponse;
    try { response = await this.sendRequest(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) {
        response = { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() };
        if (response.status === 200) return { branch: 'invalidResponse', failure: failure(operation, 'RESPONSE_TOO_LARGE', 'Insightly response exceeded the configured limit') };
      } else return { retry: true, failure: failure(operation, 'TRANSPORT', 'Insightly request failed') };
    }
    const status = response.status;
    const retryAfter = parseRetryAfter(headerValue(response.headers['retry-after']));
    if (status === 429) return { retry: true, retryAfterMs: retryAfter, failure: failure(operation, 'RATE_LIMIT', 'Insightly rate limited the request') };
    if (status === 408 || status >= 500) return { retry: true, retryAfterMs: retryAfter, failure: failure(operation, 'AVAILABILITY', 'Insightly is temporarily unavailable') };
    if (status === 404 && hasNotFound) return { branch: 'notFound', failure: failure(operation, 'NOT_FOUND', 'Insightly did not find the resource') };
    if (status !== 200) return { branch: 'providerRejected', failure: failure(operation, 'PROVIDER_REJECTION', `Insightly rejected HTTP ${status}`) };
    if (response.body.length > this.maxResponseBytes) return { branch: 'invalidResponse', failure: failure(operation, 'RESPONSE_TOO_LARGE', 'Insightly response exceeded the configured limit') };
    return { body: response.body };
  }
}

/** List Insightly contact IDs, names, and email addresses. */
export class ListContactsOperation implements Query<ListContactsInput, ContactsPage> {
  /** Bind the provider client. */ constructor(private readonly client: InsightlyClient) {}
  /** Return the manifest-generated operation definition. */ definition(): QueryDefinition { return ListContactsDefinition; }
  /** Read a bounded contact page. */
  async invoke(call: Call, input: ListContactsInput): Promise<QueryAttempt<ContactsPage>> {
    if (!input || !Number.isSafeInteger(input.top) || input.top < 1 || input.top > 500 || !Number.isSafeInteger(input.skip) || input.skip < 0) return terminal('listContacts', 'defect', 'VALIDATION', 'contact page size or offset is invalid', emptyPage());
    const result = await this.client.exchange(call, 'listContacts', `/Contacts?skip=${input.skip}&top=${input.top}`, false);
    if (result.retry) return newQueryRetry(result.failure!, result.retryAfterMs);
    if (result.failure) return newQueryBranch(result.branch!, emptyPage(), {}, result.failure);
    const document = parseJSON(result.body!);
    if (!Array.isArray(document)) return invalid('listContacts', emptyPage());
    const contacts: ContactMetadata[] = [];
    for (const item of document) {
      const contact = decodeContact(item);
      if (!contact) return invalid('listContacts', emptyPage());
      contacts.push(contact);
    }
    return newQueryBranch('listed', { contacts });
  }
}

/** Read one Insightly contact's identity without custom fields. */
export class GetContactOperation implements Query<GetContactInput, ContactMetadata> {
  /** Bind the provider client. */ constructor(private readonly client: InsightlyClient) {}
  /** Return the manifest-generated operation definition. */ definition(): QueryDefinition { return GetContactDefinition; }
  /** Validate the contact ID and read its metadata. */
  async invoke(call: Call, input: GetContactInput): Promise<QueryAttempt<ContactMetadata>> {
    if (!isValidContactID(input)) return terminal('getContact', 'defect', 'VALIDATION', 'contact ID must be a positive decimal identifier', emptyContact());
    const result = await this.client.exchange(call, 'getContact', `/Contacts/${input.contactId}`, true);
    if (result.retry) return newQueryRetry(result.failure!, result.retryAfterMs);
    if (result.failure) return newQueryBranch(result.branch!, emptyContact(), {}, result.failure);
    const contact = decodeContact(parseJSON(result.body!));
    if (!contact) return invalid('getContact', emptyContact());
    return newQueryBranch('found', contact);
  }
}

function isValidContactID(input: GetContactInput): boolean {
  if (!input || typeof input.contactId !== 'string' || !/^[0-9]+$/.test(input.contactId)) return false;
  const significantDigits = input.contactId.replace(/^0+/, '');
  return significantDigits.length > 0 && significantDigits.length <= 19 && BigInt(significantDigits) <= 9223372036854775807n;
}
function decodeContact(value: unknown): ContactMetadata | undefined {
  if (!isObject(value) || !isPositiveSafeInteger(value.CONTACT_ID)) return undefined;
  const firstName = optionalString(value.FIRST_NAME);
  const lastName = optionalString(value.LAST_NAME);
  const emailAddress = optionalString(value.EMAIL_ADDRESS);
  if (firstName === undefined || lastName === undefined || emailAddress === undefined) return undefined;
  return { id: value.CONTACT_ID, firstName, lastName, emailAddress };
}
function optionalString(value: unknown): string | undefined { return value === undefined || value === null ? '' : typeof value === 'string' ? value : undefined; }
function isPositiveSafeInteger(value: unknown): value is number { return typeof value === 'number' && Number.isSafeInteger(value) && value > 0; }
function failure(operation: string, kind: Failure['kind'], message: string): Failure { return { provider: 'insightly', operation, kind, message }; }
function terminal<Output>(operation: string, branch: string, kind: Failure['kind'], message: string, empty: Output): QueryAttempt<Output> { return newQueryBranch(branch, empty, {}, failure(operation, kind, message)); }
function invalid<Output>(operation: string, empty: Output): QueryAttempt<Output> { return terminal(operation, 'invalidResponse', 'PROTOCOL', 'Insightly returned invalid JSON', empty); }
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function parseJSON(body: Uint8Array): unknown { try { return JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { return undefined; } }
function headerValue(value: string | string[] | undefined): string { return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
