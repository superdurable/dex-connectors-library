// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetContactDefinition } from './generated/definitions.js';
import type { Config, Credentials, ContactProfile, GetContactInput } from './generated/models.js';

const emptyContact = (): ContactProfile => ({ id: '', givenName: '', familyName: '' });

/** Injectable provider sender for tests and local applications. */
export interface KeapClientOptions {
  /** Provider sender; omitted uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

interface Exchange {
  status: number;
  body: Uint8Array;
  retryAfterMs: number;
  error?: 'credential' | 'request' | 'tooLarge';
}

/** Authenticated Keap CRM v2 client for contact lookups. */
export class KeapClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate provider configuration without contacting Keap. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: KeapClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Keap configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl ?? 'https://api.infusionsoft.com/crm').replace(/\/$/, '');
    this.timeoutMs = config.timeoutMs ?? 15_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 ||
      !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('Keap request limits must be positive integers');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the getContact Query. */ getContact(): GetContactOperation { return new GetContactOperation(this); }

  /** Resolve credentials and send one bounded contact lookup. */
  async get(call: Call, input: GetContactInput): Promise<Exchange> {
    let personalAccessToken: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      personalAccessToken = credentials.personalAccessToken instanceof SecretString ? credentials.personalAccessToken.reveal() : '';
    } catch { return emptyExchange('credential'); }
    if (!isHeaderSafeCredential(personalAccessToken)) return emptyExchange('credential');
    const url = new URL(`${this.baseURL}/rest/v2/contacts/${input.contactId}`);
    url.searchParams.set('fields', 'id,given_name,family_name');
    const request: ProviderRequest = {
      url: url.toString(), method: 'GET',
      headers: { Accept: 'application/json', Authorization: `Bearer ${personalAccessToken}` },
      maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
      addressPolicy: this.baseURL.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP',
      signal: call.context.cancellationSignal,
    };
    let response: ProviderResponse;
    let isTooLarge = false;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) {
        response = { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() };
        isTooLarge = true;
      } else return emptyExchange('request');
    }
    const retryAfterMs = parseRetryAfter(header(response.headers, 'retry-after'));
    if (isTooLarge || response.body.length > this.maxResponseBytes)
      return { status: response.status, body: new Uint8Array(), retryAfterMs, error: 'tooLarge' };
    return { status: response.status, body: response.body, retryAfterMs };
  }
}

/** Read one contact by ID from Keap CRM v2. */
export class GetContactOperation implements Query<GetContactInput, ContactProfile> {
  /** Bind the provider client. */ constructor(private readonly client: KeapClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return GetContactDefinition; }
  /** Classify provider outcomes consistently with Go. */
  async invoke(call: Call, input: GetContactInput): Promise<QueryAttempt<ContactProfile>> {
    if (!isValidContactID(input?.contactId))
      return terminal('defect', 'VALIDATION', 'contactId must contain 1–128 ASCII letters, digits, hyphens, or underscores');
    const exchange = await this.client.get(call, input);
    if (exchange.error === 'credential') return terminal('defect', 'AUTHENTICATION', 'Keap credentials are unavailable or invalid');
    if (exchange.error === 'request') return retry('TRANSPORT', 'Keap request failed');
    if (exchange.status === 429) return retry('RATE_LIMIT', 'Keap rate limited the request', exchange.retryAfterMs);
    if (exchange.status === 408 || exchange.status >= 500) return retry('AVAILABILITY', 'Keap is temporarily unavailable', exchange.retryAfterMs);
    if (exchange.status === 404) return terminal('notFound', 'NOT_FOUND', 'Keap did not find the contact');
    if (exchange.status !== 200) return terminal('providerRejected', 'PROVIDER_REJECTION', `Keap rejected HTTP ${exchange.status}`);
    if (exchange.error === 'tooLarge') return terminal('invalidResponse', 'RESPONSE_TOO_LARGE', 'Keap response exceeded the configured limit');
    const contact = decodeContact(exchange.body, input.contactId);
    if (contact === undefined) return terminal('invalidResponse', 'PROTOCOL', 'Keap returned invalid contact JSON');
    return newQueryBranch('found', contact);
  }
}

function isValidContactID(value: unknown): value is string { return typeof value === 'string' && /^[A-Za-z0-9_-]{1,128}$/.test(value); }
function emptyExchange(error: NonNullable<Exchange['error']>): Exchange { return { status: 0, body: new Uint8Array(), retryAfterMs: 0, error }; }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'keap', operation: 'getContact', kind, message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<ContactProfile> { return newQueryBranch(branch, emptyContact(), {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<ContactProfile> { return newQueryRetry(failure(kind, message), delay); }
function header(headers: ProviderResponse['headers'], name: string): string {
  const entry = Object.entries(headers).find(([key]) => key.toLowerCase() === name.toLowerCase())?.[1];
  return Array.isArray(entry) ? entry[0] ?? '' : entry ?? '';
}
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function decodeContact(body: Uint8Array, contactID: string): ContactProfile | undefined {
  let raw: unknown;
  try { raw = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return undefined; }
  if (!isObject(raw) || raw.id !== contactID) return undefined;
  const givenName = raw.given_name ?? '';
  const familyName = raw.family_name ?? '';
  if (typeof givenName !== 'string' || typeof familyName !== 'string') return undefined;
  return { id: contactID, givenName, familyName };
}
