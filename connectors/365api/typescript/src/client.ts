// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListModelsDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListModelsInput, ListModelsOutput } from './generated/models.js';

const modelListEndpoint = 'https://www.hby365.studio/v1/models';
const emptyOutput: ListModelsOutput = { total: 0, modelIDs: [] };

/** Send one model-list request through a caller-owned transport. */
export type ModelRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;

/** Optional transport dependency for provider tests. */
export interface HBY365ClientOptions {
  /** Request sender; production uses the Connector SDK transport. */
  send?: ModelRequestSender;
}

/** Resolve the 365 API Key for each model-list attempt. */
export class HBY365Client {
  readonly timeoutMs: number;
  readonly maxResponseBytes: number;
  readonly credentials: CredentialProvider<Credentials>;
  readonly send: ModelRequestSender;

  /** Validate settings without making a provider request. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: HBY365ClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('365 API configuration and credential source are required');
    this.timeoutMs = config.timeoutMs ?? 15_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('365 API configuration is invalid');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the model-list Query. */
  listModels(): ListModelsOperation { return new ListModelsOperation(this); }
}

/** List at most fifty model IDs visible to the API Key. */
export class ListModelsOperation implements Query<ListModelsInput, ListModelsOutput> {
  /** Bind an authenticated 365 API client. */
  constructor(private readonly client: HBY365Client) {}

  /** Return the stable generated Query contract. */
  definition(): QueryDefinition { return ListModelsDefinition; }

  /** Send and classify one model-list request. */
  async invoke(call: Call, input: ListModelsInput): Promise<QueryAttempt<ListModelsOutput>> {
    if (input === null || typeof input !== 'object' || Array.isArray(input)) return branch('defect', 'VALIDATION', '365 API model request is invalid');
    let credentials: Credentials;
    try { credentials = await this.client.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return branch('defect', 'AUTHENTICATION', '365 API API Key is unavailable or invalid'); }
    const apiKey = credentials.apiKey instanceof SecretString ? credentials.apiKey.reveal() : '';
    if (!isValidAPIKey(apiKey)) return branch('defect', 'AUTHENTICATION', '365 API API Key is unavailable or invalid');
    let response: ProviderResponse;
    try {
      response = await this.client.send({
        url: modelListEndpoint, method: 'GET', headers: { Authorization: `Bearer ${apiKey}`, Accept: 'application/json' },
        timeoutMs: this.client.timeoutMs, maxResponseBytes: this.client.maxResponseBytes,
        addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal,
      });
    } catch (error) {
      if (error instanceof BodyTooLargeError && error.status === 200) return branch('invalidResponse', 'RESPONSE_TOO_LARGE', '365 API response exceeded the configured limit');
      if (error instanceof BodyTooLargeError && error.status !== undefined) return classifyStatus(error.status, error.headers ?? {});
      return retry('AVAILABILITY', '365 API model request failed');
    }
    if (response.status !== 200) return classifyStatus(response.status, response.headers);
    let document: unknown;
    try { document = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(response.body)) as unknown; }
    catch { return branch('invalidResponse', 'PROTOCOL', '365 API returned invalid model data'); }
    if (!isRecord(document) || document.object !== 'list' || !Array.isArray(document.data)) return branch('invalidResponse', 'PROTOCOL', '365 API returned invalid model data');
    const modelIDs: string[] = [];
    for (const model of document.data) {
      if (!isRecord(model) || model.object !== 'model' || typeof model.id !== 'string' || !model.id || Buffer.byteLength(model.id, 'utf8') > 256 || model.id.includes('\r') || model.id.includes('\n') || model.id.includes('\0')) return branch('invalidResponse', 'PROTOCOL', '365 API returned invalid model ID');
      if (modelIDs.length < 50) modelIDs.push(model.id);
    }
    return newQueryBranch('found', { total: document.data.length, modelIDs });
  }
}

function classifyStatus(status: number, headers: ProviderResponse['headers']): QueryAttempt<ListModelsOutput> {
  const retryAfterHeader = headers['retry-after'];
  const retryAfterMs = parseRetryAfter(Array.isArray(retryAfterHeader) ? retryAfterHeader[0] ?? '' : retryAfterHeader ?? '');
  if (status === 429) return retry('RATE_LIMIT', '365 API rate limited the model request', retryAfterMs);
  if (status === 408 || status >= 500) return retry('AVAILABILITY', '365 API API is temporarily unavailable', retryAfterMs);
  const kind: Failure['kind'] = status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION';
  return branch('providerRejected', kind, `365 API rejected model request with HTTP ${status}`);
}
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function isValidAPIKey(value: string): boolean { return value.length > 0 && value.length <= 1024 && /^[!-~]+$/.test(value); }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'hby365', operation: 'listModels', kind, message }; }
function branch(id: string, kind: Failure['kind'], message: string): QueryAttempt<ListModelsOutput> { return newQueryBranch(id, emptyOutput, {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delayMs = 0): QueryAttempt<ListModelsOutput> { return newQueryRetry(failure(kind, message), delayMs); }
