// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { ReauthorizationRequiredError, SecretString, newQueryBranch, newQueryRetry, resolveCredential, type Call, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { ManifestCredentialRefreshDriver } from '@superdurable/connectors-sdk/oauthtoken';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetLeadDefinition } from './generated/definitions.js';
import { Authorization, CredentialFields } from './generated/refresh.js';
import type { Config, CredentialSource, Credentials, GetLeadInput, LeadProfile } from './generated/models.js';

const emptyProfile = (): LeadProfile => ({ id: 0, firstName: '', lastName: '' });

/** Injectable provider sender for tests and local applications. */
export interface MarketoClientOptions {
  /** Provider sender; omitted uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

interface Exchange {
  status: number;
  body: Uint8Array;
  retryAfterMs: number;
  error?: 'credential' | 'request' | 'tooLarge';
}

function subscriptionURL(value: string, suffix: string): string {
  const checked = validateBaseURL(value);
  const parsed = new URL(checked);
  const isSubscription = parsed.protocol === 'https:' && parsed.hostname.toLowerCase().endsWith('.mktorest.com');
  if (!isSubscription || parsed.pathname !== suffix || parsed.search || parsed.hash || parsed.username || parsed.password || parsed.port)
    throw new TypeError(`Marketo endpoint must end in ${suffix} on a subscription host`);
  return checked.replace(/\/$/, '');
}

/** Authenticated Marketo client for selected lead fields. */
export class MarketoClient {
  private readonly restEndpoint: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly refreshDriver: ManifestCredentialRefreshDriver<Credentials>;

  /** Validate subscription endpoints without contacting Marketo. */
  constructor(config: Config, private readonly credentials: CredentialSource, options: MarketoClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Marketo configuration and credentials are required');
    this.restEndpoint = subscriptionURL(config.restEndpoint, '/rest');
    const tokenEndpoint = subscriptionURL(config.tokenEndpoint, '/identity/oauth/token');
    if (new URL(this.restEndpoint).hostname !== new URL(tokenEndpoint).hostname)
      throw new TypeError('Marketo REST and Identity URLs must use the same subscription host');
    this.timeoutMs = config.timeoutMs ?? 15_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('Marketo request limits must be positive integers');
    this.send = options.send ?? sendProviderRequest;
    this.refreshDriver = new ManifestCredentialRefreshDriver<Credentials>({
      providerName: 'Adobe Marketo Engage', auth: Authorization, credentialFields: CredentialFields,
      settings: { tokenEndpoint: config.tokenEndpoint },
    });
  }

  /** Bind the getLead Query. */ getLead(): GetLeadOperation { return new GetLeadOperation(this); }

  /** Resolve the token and perform one bounded lead lookup. */
  async get(call: Call, leadID: number): Promise<Exchange> {
    let token: string;
    try {
      const credentials = await resolveCredential(this.credentials, call, this.refreshDriver, call.context.cancellationSignal);
      token = credentials.accessToken instanceof SecretString ? credentials.accessToken.reveal() : '';
    } catch (error) { return emptyExchange(error instanceof ReauthorizationRequiredError ? 'credential' : 'request'); }
    if (!isHeaderSafeCredential(token)) return emptyExchange('credential');
    const query = new URLSearchParams({ fields: 'id,firstName,lastName' });
    const request: ProviderRequest = {
      url: `${this.restEndpoint}/v1/lead/${leadID}.json?${query}`, method: 'GET',
      headers: { Accept: 'application/json', Authorization: `Bearer ${token}` },
      maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
      addressPolicy: 'publicHTTPS',
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

/** Read one lead's ID and names from Marketo. */
export class GetLeadOperation implements Query<GetLeadInput, LeadProfile> {
  /** Bind one Marketo client. */ constructor(private readonly client: MarketoClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return GetLeadDefinition; }
  /** Classify lead lookup outcomes consistently with Go. */
  async invoke(call: Call, input: GetLeadInput): Promise<QueryAttempt<LeadProfile>> {
    if (!Number.isSafeInteger(input?.leadId) || input.leadId <= 0)
      return terminal('defect', 'VALIDATION', 'Marketo lead ID must be positive');
    const exchange = await this.client.get(call, input.leadId);
    if (exchange.error === 'credential') return terminal('defect', 'AUTHENTICATION', 'Marketo connection needs authorization');
    if (exchange.error === 'request') return retry('TRANSPORT', 'Marketo request failed');
    if (exchange.status === 429) return retry('RATE_LIMIT', 'Marketo rate limited the request', exchange.retryAfterMs);
    if (exchange.status === 408 || exchange.status >= 500) return retry('AVAILABILITY', 'Marketo is temporarily unavailable', exchange.retryAfterMs);
    if (exchange.status !== 200) return terminal('providerRejected', 'PROVIDER_REJECTION', `Marketo rejected HTTP ${exchange.status}`);
    if (exchange.error === 'tooLarge') return terminal('invalidResponse', 'RESPONSE_TOO_LARGE', 'Marketo response exceeded the configured limit');
    const decoded = decodeLead(exchange.body, input.leadId);
    if (decoded.kind === 'found') return newQueryBranch('found', decoded.profile);
    if (decoded.kind === 'rateLimit') return retry('RATE_LIMIT', 'Marketo quota or concurrency limit was reached', exchange.retryAfterMs);
    if (decoded.kind === 'availability') return retry('AVAILABILITY', 'Marketo is temporarily unavailable', exchange.retryAfterMs);
    return terminal(decoded.kind, decoded.failure, 'Marketo lead lookup did not return a profile');
  }
}

type Decoded = { kind: 'found'; profile: LeadProfile } | { kind: 'rateLimit' } | { kind: 'availability' } | { kind: 'notFound' | 'providerRejected' | 'invalidResponse'; failure: Failure['kind'] };
function decodeLead(body: Uint8Array, requestedID: number): Decoded {
  let raw: unknown;
  try { raw = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; }
  catch { return { kind: 'invalidResponse', failure: 'PROTOCOL' }; }
  if (!isObject(raw) || typeof raw.success !== 'boolean') return { kind: 'invalidResponse', failure: 'PROTOCOL' };
  if (!raw.success) {
    if (!Array.isArray(raw.errors) || raw.errors.length === 0 || !isObject(raw.errors[0]) || typeof raw.errors[0].code !== 'string')
      return { kind: 'invalidResponse', failure: 'PROTOCOL' };
    const code = raw.errors[0].code;
    if (['606', '607', '615'].includes(code)) return { kind: 'rateLimit' };
    if (['604', '608', '611', '614', '500', '502'].includes(code)) return { kind: 'availability' };
    if (code === '702') return { kind: 'notFound', failure: 'NOT_FOUND' };
    return { kind: 'providerRejected', failure: ['601', '602'].includes(code) ? 'AUTHENTICATION' : 'PROVIDER_REJECTION' };
  }
  if (!Array.isArray(raw.result) || raw.result.length > 1) return { kind: 'invalidResponse', failure: 'PROTOCOL' };
  if (raw.result.length === 0) return { kind: 'notFound', failure: 'NOT_FOUND' };
  const lead: unknown = raw.result[0];
  if (!isObject(lead) || !Number.isSafeInteger(lead.id) || lead.id !== requestedID || !optionalString(lead.firstName) || !optionalString(lead.lastName))
    return { kind: 'invalidResponse', failure: 'PROTOCOL' };
  return { kind: 'found', profile: { id: requestedID, firstName: typeof lead.firstName === 'string' ? lead.firstName : '', lastName: typeof lead.lastName === 'string' ? lead.lastName : '' } };
}
function optionalString(value: unknown): boolean { return value === undefined || value === null || typeof value === 'string'; }
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function emptyExchange(error: NonNullable<Exchange['error']>): Exchange { return { status: 0, body: new Uint8Array(), retryAfterMs: 0, error }; }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'adobe-marketo', operation: 'getLead', kind, message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<LeadProfile> { return newQueryBranch(branch, emptyProfile(), {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<LeadProfile> { return newQueryRetry(failure(kind, message), delay); }
function header(headers: ProviderResponse['headers'], name: string): string {
  const entry = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1];
  return Array.isArray(entry) ? entry[0] ?? '' : entry ?? '';
}
