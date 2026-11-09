// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { FindContactDefinition } from './generated/definitions.js';
import type { Config, Credentials, Contact, FindContactInput } from './generated/models.js';

const emptyContact = (): Contact => ({ contactId: '', fullName: '', emailAddress: '' });
const uuidPattern = /^[0-9a-fA-F]{8}(?:-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}$/;
const emailPattern = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

/** Injectable provider sender and token endpoint for tests. */
export interface MicrosoftDynamics365SalesClientOptions {
  /** Provider sender; omitted uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Token endpoint; omitted uses the Entra tenant endpoint. */ tokenEndpoint?: string;
}
interface Exchange { status: number; body: Uint8Array; retryAfterMs: number; error?: 'credential' | 'request' | 'tooLarge' | 'invalidToken' }

/** Dataverse client that acquires a fresh Entra token for each Query attempt. */
export class MicrosoftDynamics365SalesClient {
  private readonly environmentUrl: string;
  private readonly clientId: string;
  private readonly tokenEndpoint: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate the Dataverse environment and Entra identifiers. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: MicrosoftDynamics365SalesClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Dynamics 365 Sales configuration and credentials are required');
    if (!uuidPattern.test(config.tenantId) || !uuidPattern.test(config.clientId)) throw new TypeError('Dynamics 365 Sales tenant and client IDs must be UUIDs');
    this.clientId = config.clientId;
    this.environmentUrl = validateBaseURL(config.environmentUrl).replace(/\/+$/, '');
    const environment = new URL(this.environmentUrl);
    if (environment.pathname !== '/' || !(environment.hostname.endsWith('.dynamics.com') || ['localhost', '127.0.0.1', '::1'].includes(environment.hostname)))
      throw new TypeError('Dynamics 365 Sales environment URL must be a Dataverse origin');
    this.tokenEndpoint = options.tokenEndpoint ?? `https://login.microsoftonline.com/${config.tenantId}/oauth2/v2.0/token`;
    const token = new URL(this.tokenEndpoint);
    if (token.username || token.password || token.search || token.hash || (token.protocol !== 'https:' && !(token.protocol === 'http:' && ['localhost', '127.0.0.1', '::1'].includes(token.hostname))))
      throw new TypeError('Dynamics 365 Sales token endpoint is invalid');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('Dynamics 365 Sales request limits must be positive integers');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the findContact Query. */ findContact(): FindContactOperation { return new FindContactOperation(this); }

  private async request(call: Call, method: string, url: string, headers: Record<string, string>, body?: Uint8Array): Promise<Exchange> {
    const request: ProviderRequest = { url, method, headers, ...(body ? { body } : {}),
      maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
      addressPolicy: url.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP',
      signal: call.context.cancellationSignal };
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

  /** Acquire a token and query at most two matching contacts. */
  async queryContacts(call: Call, emailAddress: string): Promise<Exchange> {
    let clientSecret: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      clientSecret = credentials.clientSecret instanceof SecretString ? credentials.clientSecret.reveal() : '';
    } catch { return emptyExchange('credential'); }
    if (!isHeaderSafeCredential(clientSecret)) return emptyExchange('credential');
    const form = new URLSearchParams({ client_id: this.clientId, client_secret: clientSecret, scope: `${this.environmentUrl}/.default`, grant_type: 'client_credentials' }).toString();
    const token = await this.request(call, 'POST', this.tokenEndpoint, { 'Content-Type': 'application/x-www-form-urlencoded' }, Buffer.from(form));
    if (token.status !== 200 || token.error) return token;
    let accessToken: string;
    try {
      const parsed: unknown = JSON.parse(Buffer.from(token.body).toString('utf8'));
      if (!parsed || typeof parsed !== 'object') return { ...emptyExchange('invalidToken'), status: 200 };
      const fields = parsed as Record<string, unknown>;
      if (fields.token_type !== 'Bearer' || typeof fields.access_token !== 'string' || !isHeaderSafeCredential(fields.access_token))
        return { ...emptyExchange('invalidToken'), status: 200 };
      accessToken = fields.access_token;
    } catch { return { ...emptyExchange('invalidToken'), status: 200 }; }
    const filter = `emailaddress1 eq '${emailAddress.replaceAll("'", "''")}'`;
    const query = new URLSearchParams({ '$select': 'contactid,fullname,emailaddress1', '$filter': filter, '$top': '2' }).toString();
    return this.request(call, 'GET', `${this.environmentUrl}/api/data/v9.2/contacts?${query}`, { Authorization: `Bearer ${accessToken}`, Accept: 'application/json', 'OData-Version': '4.0', 'OData-MaxVersion': '4.0' });
  }

}

/** Find exactly one Dataverse contact by primary email address. */
export class FindContactOperation implements Query<FindContactInput, Contact> {
  /** Bind the provider client. */ constructor(private readonly client: MicrosoftDynamics365SalesClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return FindContactDefinition; }
  /** Classify token, transport, and contact lookup outcomes. */
  async invoke(call: Call, input: FindContactInput): Promise<QueryAttempt<Contact>> {
    if (!input || typeof input.emailAddress !== 'string' || input.emailAddress.length > 320 || !emailPattern.test(input.emailAddress))
      return terminal('defect', 'VALIDATION', 'contact email address is invalid');
    const exchange = await this.client.queryContacts(call, input.emailAddress);
    if (exchange.error === 'credential') return terminal('defect', 'AUTHENTICATION', 'client credentials are unavailable or invalid');
    if (exchange.error === 'request') return retry('TRANSPORT', 'Microsoft request failed');
    if (exchange.status === 429) return retry('RATE_LIMIT', 'Microsoft rate limited the request', exchange.retryAfterMs);
    if (exchange.status === 408 || exchange.status >= 500) return retry('AVAILABILITY', 'Microsoft is temporarily unavailable', exchange.retryAfterMs);
    if (exchange.error === 'tooLarge') return terminal('invalidResponse', 'RESPONSE_TOO_LARGE', 'Microsoft response exceeded the configured limit');
    if (exchange.error === 'invalidToken') return terminal('invalidResponse', 'PROTOCOL', 'Entra returned an invalid access token');
    if (exchange.status !== 200) return terminal('providerRejected', 'PROVIDER_REJECTION', `Microsoft rejected HTTP ${exchange.status}`);
    let records: unknown;
    try {
      const payload: unknown = JSON.parse(Buffer.from(exchange.body).toString('utf8'));
      records = (payload as { value?: unknown }).value;
    } catch { return terminal('invalidResponse', 'PROTOCOL', 'Dataverse returned invalid contacts JSON'); }
    if (!Array.isArray(records) || records.length > 2) return terminal('invalidResponse', 'PROTOCOL', 'Dataverse returned invalid contacts JSON');
    if (records.length === 0) return newQueryBranch('missing', emptyContact());
    if (records.length > 1) return newQueryBranch('multipleMatches', emptyContact());
    const record: unknown = records[0];
    if (!record || typeof record !== 'object') return terminal('invalidResponse', 'PROTOCOL', 'Dataverse returned an invalid contact');
    const contact = record as Record<string, unknown>;
    if (typeof contact.contactid !== 'string' || !uuidPattern.test(contact.contactid) || typeof contact.emailaddress1 !== 'string' || contact.emailaddress1.toLowerCase() !== input.emailAddress.toLowerCase())
      return terminal('invalidResponse', 'PROTOCOL', 'Dataverse returned an invalid contact');
    return newQueryBranch('found', { contactId: contact.contactid, fullName: typeof contact.fullname === 'string' ? contact.fullname : '', emailAddress: contact.emailaddress1 });
  }
}
function emptyExchange(error: NonNullable<Exchange['error']>): Exchange { return { status: 0, body: new Uint8Array(), retryAfterMs: 0, error }; }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'microsoft-dynamics-365-sales', operation: 'findContact', kind, message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<Contact> { return newQueryBranch(branch, emptyContact(), {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<Contact> { return newQueryRetry(failure(kind, message), delay); }
function header(headers: ProviderResponse['headers'], name: string): string {
  const entry = Object.entries(headers).find(([key]) => key.toLowerCase() === name.toLowerCase())?.[1];
  return Array.isArray(entry) ? entry[0] ?? '' : entry ?? '';
}
