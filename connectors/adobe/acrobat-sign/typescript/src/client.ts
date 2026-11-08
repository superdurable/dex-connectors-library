// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetAgreementDefinition } from './generated/definitions.js';
import type { Config, Credentials, AgreementStatus, GetAgreementInput } from './generated/models.js';

const defaultBaseURL = 'https://api.adobesign.com/api/rest/v6';
const emptyAgreement = (): AgreementStatus => ({ id: '', name: '', status: '' });

/** Injectable provider sender for tests and local applications. */
export interface AcrobatSignClientOptions {
  /** Provider sender; omitted uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

interface Exchange {
  status: number;
  body: Uint8Array;
  retryAfterMs: number;
  error?: 'credential' | 'request' | 'tooLarge' | 'invalidAccessPoint';
  discovery?: boolean;
}

/** Authenticated Adobe Acrobat Sign REST API v6 client for agreement metadata. */
export class AcrobatSignClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private cachedKeyHash = '';
  private cachedAccessPoint = '';

  /** Validate provider configuration without contacting AcrobatSign. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: AcrobatSignClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('AcrobatSign configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl || defaultBaseURL);
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 ||
      !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('AcrobatSign request limits must be positive integers');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the getAgreement Query. */ getAgreement(): GetAgreementOperation { return new GetAgreementOperation(this); }

  /** Resolve the integration key, discover the regional API, and read one agreement. */
  async get(call: Call, agreementID: string): Promise<Exchange> {
    let integrationKey: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      integrationKey = credentials.integrationKey instanceof SecretString ? credentials.integrationKey.reveal() : '';
    } catch { return emptyExchange('credential'); }
    if (!integrationKey || !isHeaderSafeCredential(integrationKey)) return emptyExchange('credential');
    const keyHash = createHash('sha256').update(integrationKey).digest('hex');
    let accessPoint = this.cachedKeyHash === keyHash ? this.cachedAccessPoint : '';
    if (!accessPoint) {
      const discovery = await this.request(call, `${this.baseURL}/baseUris`, integrationKey);
      if (discovery.status !== 200 || discovery.error) return { ...discovery, discovery: true };
      try {
        const payload: unknown = JSON.parse(Buffer.from(discovery.body).toString('utf8')) as unknown;
        if (!isObject(payload)) throw new TypeError('missing access point');
        accessPoint = validateAccessPoint(payload.apiAccessPoint, this.baseURL);
      } catch { return { ...emptyExchange('invalidAccessPoint'), status: 200, discovery: true }; }
      this.cachedAccessPoint = accessPoint;
      this.cachedKeyHash = keyHash;
    }
    return this.request(call, `${accessPoint}/api/rest/v6/agreements/${encodeURIComponent(agreementID)}`, integrationKey);
  }

  private async request(call: Call, url: string, integrationKey: string): Promise<Exchange> {
    const request: ProviderRequest = {
      url, method: 'GET',
      headers: { Accept: 'application/json', Authorization: `Bearer ${integrationKey}` },
      maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
      addressPolicy: url.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP',
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

/** Read one agreement's ID, name, and status. */
export class GetAgreementOperation implements Query<GetAgreementInput, AgreementStatus> {
  /** Bind the provider client. */ constructor(private readonly client: AcrobatSignClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return GetAgreementDefinition; }
  /** Classify provider outcomes consistently with the Go implementation. */
  async invoke(call: Call, input: GetAgreementInput): Promise<QueryAttempt<AgreementStatus>> {
    if (!isValidAgreementID(input?.agreementId))
      return terminal('defect', 'VALIDATION', 'agreement ID must be 1–256 printable characters');
    const exchange = await this.client.get(call, input.agreementId);
    if (exchange.error === 'credential') return terminal('defect', 'AUTHENTICATION', 'AcrobatSign credentials are unavailable or invalid');
    if (exchange.error === 'request') return retry('TRANSPORT', 'AcrobatSign request failed');
    if (exchange.status === 429) return retry('RATE_LIMIT', 'AcrobatSign rate limited the request', exchange.retryAfterMs);
    if (exchange.status === 408 || exchange.status >= 500) return retry('AVAILABILITY', 'AcrobatSign is temporarily unavailable', exchange.retryAfterMs);
    if (exchange.error === 'invalidAccessPoint') return terminal('invalidResponse', 'PROTOCOL', 'Acrobat Sign returned an unsafe API access point');
    if (exchange.error === 'tooLarge') return terminal('invalidResponse', 'RESPONSE_TOO_LARGE', 'Acrobat Sign response exceeded the configured limit');
    if (exchange.discovery && exchange.status !== 200) return terminal('providerRejected', 'PROVIDER_REJECTION', `Acrobat Sign rejected discovery HTTP ${exchange.status}`);
    if (exchange.status === 404) return terminal('notFound', 'NOT_FOUND', 'AcrobatSign did not expose the agreement');
    if (exchange.status !== 200) return terminal('providerRejected', 'PROVIDER_REJECTION', `AcrobatSign rejected HTTP ${exchange.status}`);
    const agreement = decodeAgreement(exchange.body, input.agreementId);
    if (!agreement) return terminal('invalidResponse', 'PROTOCOL', 'AcrobatSign returned invalid agreement JSON');
    return newQueryBranch('found', agreement);
  }
}

function isValidAgreementID(value: unknown): value is string {
  if (typeof value !== 'string' || !value || Buffer.byteLength(value, 'utf8') > 256) return false;
  for (const character of value) {
    const codePoint = character.codePointAt(0) ?? 0;
    if (codePoint < 32 || (codePoint >= 127 && codePoint <= 159) || '/?#'.includes(character)) return false;
  }
  return true;
}
function emptyExchange(error: NonNullable<Exchange['error']>): Exchange { return { status: 0, body: new Uint8Array(), retryAfterMs: 0, error }; }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'adobe-acrobat-sign', operation: 'getAgreement', kind, message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<AgreementStatus> { return newQueryBranch(branch, emptyAgreement(), {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<AgreementStatus> { return newQueryRetry(failure(kind, message), delay); }
function header(headers: ProviderResponse['headers'], name: string): string {
  const entry = Object.entries(headers).find(([key]) => key.toLowerCase() === name.toLowerCase())?.[1];
  return Array.isArray(entry) ? entry[0] ?? '' : entry ?? '';
}
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function validateAccessPoint(raw: unknown, discoveryURL: string): string {
  if (typeof raw !== 'string') throw new TypeError('invalid access point');
  const accessPoint = new URL(raw);
  const discovery = new URL(discoveryURL);
  if (accessPoint.username || accessPoint.password || accessPoint.search || accessPoint.hash || accessPoint.pathname !== '/') throw new TypeError('invalid access point');
  const isLoopback = ['localhost', '127.0.0.1', '[::1]'].includes(discovery.hostname);
  if (isLoopback) {
    if (accessPoint.origin !== discovery.origin) throw new TypeError('invalid local access point');
  } else if (accessPoint.protocol !== 'https:' || accessPoint.port || !accessPoint.hostname.startsWith('api.') ||
    !(accessPoint.hostname.endsWith('.adobesign.com') || accessPoint.hostname.endsWith('.echosign.com'))) throw new TypeError('invalid Adobe access point');
  return accessPoint.origin;
}
function decodeAgreement(body: Uint8Array, agreementID: string): AgreementStatus | undefined {
  let raw: unknown;
  try { raw = JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { return undefined; }
  if (!isObject(raw) || raw.id !== agreementID || typeof raw.name !== 'string' || !raw.name.trim() || typeof raw.status !== 'string' || !raw.status.trim()) return undefined;
  return { id: agreementID, name: raw.name, status: raw.status };
}
