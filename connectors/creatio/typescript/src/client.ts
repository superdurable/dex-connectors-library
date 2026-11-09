// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { ReauthorizationRequiredError, SecretString, newQueryBranch, newQueryRetry, resolveCredential, type Call, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { ManifestCredentialRefreshDriver } from '@superdurable/connectors-sdk/oauthtoken';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetContactDefinition } from './generated/definitions.js';
import { Authorization, CredentialFields } from './generated/refresh.js';
import type { ContactSummary, Config, CredentialSource, Credentials, GetContactInput } from './generated/models.js';

const contactUUIDPattern = /^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$/;
const emptyContact = (): ContactSummary => ({ id: '', name: '', email: '' });

/** Injectable provider sender for tests and local applications. */
export interface CreatioClientOptions {
  /** Provider sender; omitted uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

interface Exchange {
  status: number;
  body: Uint8Array;
  retryAfterMs: number;
  error?: 'credential' | 'refresh' | 'request' | 'tooLarge';
}

/** Authenticated Creatio OData 4 client for selected contact fields. */
export class CreatioClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly refreshDriver: ManifestCredentialRefreshDriver<Credentials>;

  /** Validate instance and Identity Service URLs without contacting Creatio. */
  constructor(config: Config, private readonly credentials: CredentialSource, options: CreatioClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Creatio configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl).replace(/\/$/, '');
    const tokenEndpoint = validateBaseURL(config.tokenEndpoint);
    const tokenURL = new URL(tokenEndpoint);
    if (!tokenURL.pathname.endsWith('/connect/token') || tokenURL.search || tokenURL.hash)
      throw new TypeError('Creatio Identity Service token URL must end in /connect/token');
    this.timeoutMs = config.timeoutMs ?? 15_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('Creatio request limits must be positive integers');
    this.send = options.send ?? sendProviderRequest;
    this.refreshDriver = new ManifestCredentialRefreshDriver<Credentials>({ providerName: 'Creatio', auth: Authorization,
      credentialFields: CredentialFields, settings: { tokenEndpoint } });
  }

  /** Bind the getContact Query. */ getContact(): GetContactOperation { return new GetContactOperation(this); }

  /** Resolve a renewable token and perform one bounded OData Contact query. */
  async get(call: Call, contactID: string): Promise<Exchange> {
    let token: string;
    try {
      const credentials = await resolveCredential(this.credentials, call, this.refreshDriver, call.context.cancellationSignal);
      token = credentials.accessToken instanceof SecretString ? credentials.accessToken.reveal() : '';
    } catch (error) { return emptyExchange(error instanceof ReauthorizationRequiredError ? 'credential' : 'refresh'); }
    if (!isHeaderSafeCredential(token)) return emptyExchange('credential');
    const query = new URLSearchParams({ '$filter': `Id eq ${contactID}`, '$select': 'Id,Name,Email', '$top': '1' });
    const request: ProviderRequest = {
      url: `${this.baseURL}/0/odata/Contact?${query}`, method: 'GET',
      headers: { Accept: 'application/json', Authorization: `Bearer ${token}` },
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
    if (Buffer.from(response.body).includes(token)) return { ...emptyExchange('credential'), status: response.status, retryAfterMs };
    return { status: response.status, body: response.body, retryAfterMs };
  }
}

/** Read one contact's UUID, name, and email from Creatio. */
export class GetContactOperation implements Query<GetContactInput, ContactSummary> {
  /** Bind one Creatio client. */ constructor(private readonly client: CreatioClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return GetContactDefinition; }
  /** Classify contact lookup outcomes consistently with Go. */
  async invoke(call: Call, input: GetContactInput): Promise<QueryAttempt<ContactSummary>> {
    if (typeof input?.contactId !== 'string' || !contactUUIDPattern.test(input.contactId))
      return terminal('defect', 'VALIDATION', 'Creatio contact UUID is invalid');
    const exchange = await this.client.get(call, input.contactId);
    if (exchange.error === 'credential') return terminal('defect', 'AUTHENTICATION', 'Creatio connection needs authorization');
    if (exchange.error === 'refresh') return retry('TRANSPORT', 'Creatio credential minting failed');
    if (exchange.error === 'request') return retry('TRANSPORT', 'Creatio request failed');
    if (exchange.status === 429) return retry('RATE_LIMIT', 'Creatio rate limited the request', exchange.retryAfterMs);
    if (exchange.status === 408 || exchange.status >= 500) return retry('AVAILABILITY', 'Creatio is temporarily unavailable', exchange.retryAfterMs);
    if (exchange.status === 404) return terminal('notFound', 'NOT_FOUND', 'Creatio contact was not found');
    if (exchange.status !== 200) return terminal('providerRejected', 'PROVIDER_REJECTION', `Creatio rejected HTTP ${exchange.status}`);
    if (exchange.error === 'tooLarge') return terminal('invalidResponse', 'RESPONSE_TOO_LARGE', 'Creatio response exceeded the configured limit');
    const decoded = decodeContact(exchange.body, input.contactId);
    if (decoded === undefined) return terminal('invalidResponse', 'PROTOCOL', 'Creatio returned invalid contact JSON');
    if (decoded === null) return terminal('notFound', 'NOT_FOUND', 'Creatio contact was not found');
    return newQueryBranch('found', decoded);
  }
}

function emptyExchange(error: NonNullable<Exchange['error']>): Exchange { return { status: 0, body: new Uint8Array(), retryAfterMs: 0, error }; }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'creatio', operation: 'getContact', kind, message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<ContactSummary> { return newQueryBranch(branch, emptyContact(), {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<ContactSummary> { return newQueryRetry(failure(kind, message), delay); }
function header(headers: ProviderResponse['headers'], name: string): string {
  const entry = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1];
  return Array.isArray(entry) ? entry[0] ?? '' : entry ?? '';
}
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function decodeContact(body: Uint8Array, requestedID: string): ContactSummary | null | undefined {
  let document: unknown;
  try { document = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return undefined; }
  if (!isObject(document) || !Array.isArray(document.value)) return undefined;
  if (document.value.length === 0) return null;
  if (document.value.length !== 1 || !isObject(document.value[0])) return undefined;
  const record = document.value[0];
  if (typeof record.Id !== 'string' || !contactUUIDPattern.test(record.Id) || record.Id.toLowerCase() !== requestedID.toLowerCase() ||
    typeof record.Name !== 'string' || !record.Name.trim() ||
    record.Email !== null && record.Email !== undefined && typeof record.Email !== 'string') return undefined;
  return { id: record.Id, name: record.Name, email: typeof record.Email === 'string' ? record.Email : '' };
}
