// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListSpacesDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListSpacesInput, ListSpacesOutput } from './generated/models.js';

const spaceListEndpoint = 'https://api.huoban.com/openapi/v1/space/list';
const emptyOutput: ListSpacesOutput = { spaces: [] };

/** Send one space-list request through a caller-owned transport. */
export type SpaceRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;

/** Optional transport dependency for provider tests. */
export interface HuobanClientOptions {
  /** Request sender; production uses the Connector SDK transport. */
  send?: SpaceRequestSender;
}

/** Resolve the Huoban Key for each space-list attempt. */
export class HuobanClient {
  readonly timeoutMs: number;
  readonly maxResponseBytes: number;
  readonly credentials: CredentialProvider<Credentials>;
  readonly send: SpaceRequestSender;

  /** Validate settings without making a provider request. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: HuobanClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Huoban configuration and credential source are required');
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('Huoban configuration is invalid');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the space-list Query. */
  listSpaces(): ListSpacesOperation { return new ListSpacesOperation(this); }
}

/** List workspace IDs and names visible to the API Key. */
export class ListSpacesOperation implements Query<ListSpacesInput, ListSpacesOutput> {
  /** Bind an authenticated Huoban client. */
  constructor(private readonly client: HuobanClient) {}

  /** Return the stable generated Query contract. */
  definition(): QueryDefinition { return ListSpacesDefinition; }

  /** Send and classify one space-list request. */
  async invoke(call: Call, input: ListSpacesInput): Promise<QueryAttempt<ListSpacesOutput>> {
    if (input === null || typeof input !== 'object' || Array.isArray(input)) return branch('defect', 'VALIDATION', 'Huoban workspace request is invalid');
    let credentials: Credentials;
    try { credentials = await this.client.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return branch('defect', 'AUTHENTICATION', 'Huoban API Key is unavailable or invalid'); }
    const apiKey = credentials.apiKey instanceof SecretString ? credentials.apiKey.reveal() : '';
    if (!isValidAPIKey(apiKey)) return branch('defect', 'AUTHENTICATION', 'Huoban API Key is unavailable or invalid');
    let response: ProviderResponse;
    try {
      response = await this.client.send({
        url: spaceListEndpoint, method: 'GET', headers: { 'Open-Authorization': `Bearer ${apiKey}`, Accept: 'application/json' },
        timeoutMs: this.client.timeoutMs, maxResponseBytes: this.client.maxResponseBytes,
        addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal,
      });
    } catch (error) {
      if (error instanceof BodyTooLargeError && error.status === 200) return branch('invalidResponse', 'RESPONSE_TOO_LARGE', 'Huoban response exceeded the configured limit');
      if (error instanceof BodyTooLargeError && error.status !== undefined) return classifyStatus(error.status, error.headers ?? {});
      return retry('AVAILABILITY', 'Huoban workspace request failed');
    }
    if (response.status !== 200) return classifyStatus(response.status, response.headers);
    let document: unknown;
    try { document = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(response.body)) as unknown; }
    catch { return branch('invalidResponse', 'PROTOCOL', 'Huoban returned invalid workspace metadata'); }
    if (!isRecord(document) || !Number.isSafeInteger(document.code)) return branch('invalidResponse', 'PROTOCOL', 'Huoban returned invalid workspace metadata');
    if (document.code !== 0) return branch('providerRejected', 'PROVIDER_REJECTION', `Huoban rejected workspace list with code ${document.code}`);
    if (!isRecord(document.data) || !Array.isArray(document.data.spaces)) return branch('invalidResponse', 'PROTOCOL', 'Huoban returned incomplete workspace metadata');
    const spaces: NonNullable<ListSpacesOutput['spaces']> = [];
    for (const item of document.data.spaces) {
      if (!isRecord(item) || typeof item.space_id !== 'string' || !item.space_id || typeof item.name !== 'string' || !item.name) return branch('invalidResponse', 'PROTOCOL', 'Huoban returned an incomplete workspace');
      spaces.push({ spaceId: item.space_id, name: item.name });
    }
    return newQueryBranch('listed', { spaces });
  }
}

function classifyStatus(status: number, headers: ProviderResponse['headers']): QueryAttempt<ListSpacesOutput> {
  const retryAfterHeader = headers['retry-after'];
  const retryAfterMs = parseRetryAfter(Array.isArray(retryAfterHeader) ? retryAfterHeader[0] ?? '' : retryAfterHeader ?? '');
  if (status === 429) return retry('RATE_LIMIT', 'Huoban rate limited the workspace request', retryAfterMs);
  if (status === 408 || status >= 500) return retry('AVAILABILITY', 'Huoban API is temporarily unavailable', retryAfterMs);
  const kind: Failure['kind'] = status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION';
  return branch('providerRejected', kind, `Huoban rejected workspace request with HTTP ${status}`);
}
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function isValidAPIKey(value: string): boolean { return value.length > 0 && value.length <= 1024 && /^[!-~]+$/.test(value); }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'huoban', operation: 'listSpaces', kind, message }; }
function branch(id: string, kind: Failure['kind'], message: string): QueryAttempt<ListSpacesOutput> { return newQueryBranch(id, emptyOutput, {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delayMs = 0): QueryAttempt<ListSpacesOutput> { return newQueryRetry(failure(kind, message), delayMs); }
