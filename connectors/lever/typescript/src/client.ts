// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetOpportunityDefinition } from './generated/definitions.js';
import type { Config, Credentials, OpportunityStage, GetOpportunityInput } from './generated/models.js';

const defaultBaseURL = 'https://api.lever.co/v1';
const emptyOpportunity = (): OpportunityStage => ({ id: '', name: '', stage: '' });

/** Injectable provider sender for tests and local applications. */
export interface LeverClientOptions {
  /** Provider sender; omitted uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

interface Exchange {
  status: number;
  body: Uint8Array;
  retryAfterMs: number;
  error?: 'credential' | 'request' | 'tooLarge';
}

/** Authenticated Lever REST API client for opportunity metadata. */
export class LeverClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate provider configuration without contacting Lever. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: LeverClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Lever configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl || defaultBaseURL);
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 ||
      !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('Lever request limits must be positive integers');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the getOpportunity Query. */ getOpportunity(): GetOpportunityOperation { return new GetOpportunityOperation(this); }

  /** Resolve the API key and send one bounded opportunity lookup. */
  async get(call: Call, opportunityID: string): Promise<Exchange> {
    let apiKey: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      apiKey = credentials.apiKey instanceof SecretString ? credentials.apiKey.reveal() : '';
    } catch { return emptyExchange('credential'); }
    if (!apiKey || !isHeaderSafeCredential(apiKey) || apiKey.includes(':')) return emptyExchange('credential');
    const request: ProviderRequest = {
      url: `${this.baseURL}/opportunities/${encodeURIComponent(opportunityID)}`, method: 'GET',
      headers: { Accept: 'application/json', Authorization: `Basic ${Buffer.from(apiKey + ':').toString('base64')}` },
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

/** Read one opportunity's ID, name, and stage. */
export class GetOpportunityOperation implements Query<GetOpportunityInput, OpportunityStage> {
  /** Bind the provider client. */ constructor(private readonly client: LeverClient) {}
  /** Return the generated Query opportunity. */ definition(): QueryDefinition { return GetOpportunityDefinition; }
  /** Classify provider outcomes consistently with the Go implementation. */
  async invoke(call: Call, input: GetOpportunityInput): Promise<QueryAttempt<OpportunityStage>> {
    if (!isValidOpportunityID(input?.opportunityId))
      return terminal('defect', 'VALIDATION', 'opportunity ID must be a UUID');
    const exchange = await this.client.get(call, input.opportunityId);
    if (exchange.error === 'credential') return terminal('defect', 'AUTHENTICATION', 'Lever credentials are unavailable or invalid');
    if (exchange.error === 'request') return retry('TRANSPORT', 'Lever request failed');
    if (exchange.status === 429) return retry('RATE_LIMIT', 'Lever rate limited the request', exchange.retryAfterMs);
    if (exchange.status === 408 || exchange.status >= 500) return retry('AVAILABILITY', 'Lever is temporarily unavailable', exchange.retryAfterMs);
    if (exchange.status === 404) return terminal('notFound', 'NOT_FOUND', 'Lever did not expose the opportunity');
    if (exchange.status !== 200) return terminal('providerRejected', 'PROVIDER_REJECTION', `Lever rejected HTTP ${exchange.status}`);
    if (exchange.error === 'tooLarge') return terminal('invalidResponse', 'RESPONSE_TOO_LARGE', 'Lever response exceeded the configured limit');
    const opportunityStatus = decodeOpportunity(exchange.body, input.opportunityId);
    if (!opportunityStatus) return terminal('invalidResponse', 'PROTOCOL', 'Lever returned invalid opportunity JSON');
    return newQueryBranch('found', opportunityStatus);
  }
}

function isValidOpportunityID(value: unknown): value is string {
  return typeof value === 'string' && /^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-8][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$/.test(value);
}
function emptyExchange(error: NonNullable<Exchange['error']>): Exchange { return { status: 0, body: new Uint8Array(), retryAfterMs: 0, error }; }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'lever', operation: 'getOpportunity', kind, message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<OpportunityStage> { return newQueryBranch(branch, emptyOpportunity(), {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<OpportunityStage> { return newQueryRetry(failure(kind, message), delay); }
function header(headers: ProviderResponse['headers'], name: string): string {
  const entry = Object.entries(headers).find(([key]) => key.toLowerCase() === name.toLowerCase())?.[1];
  return Array.isArray(entry) ? entry[0] ?? '' : entry ?? '';
}
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function decodeOpportunity(body: Uint8Array, opportunityID: string): OpportunityStage | undefined {
  let raw: unknown;
  try { raw = JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { return undefined; }
  if (!isObject(raw) || !isObject(raw.data)) return undefined;
  const data = raw.data;
  if (data.id !== opportunityID || typeof data.stage !== 'string' || !data.stage.trim() ||
    typeof data.name !== 'string' || !data.name.trim()) return undefined;
  return { id: opportunityID, name: data.name, stage: data.stage };
}
