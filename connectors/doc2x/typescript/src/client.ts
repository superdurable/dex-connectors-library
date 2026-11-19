// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetParseStatusDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetParseStatusInput, GetParseStatusOutput } from './generated/models.js';

const endpoint = 'https://v2.doc2x.noedgeai.com/api/v2/parse/status';
const emptyOutput: GetParseStatusOutput = { status: '', progress: 0 };

/** Optional provider sender for isolated conformance replay. */
export interface Doc2xClientOptions {
  /** Sender; omitted uses the SDK HTTPS transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

/** Read a Doc2X PDF task's status without returning document contents. */
export class Doc2xClient {
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate local settings without contacting Doc2X. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: Doc2xClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Doc2X configuration and credentials are required');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('Doc2X request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the parse-status Query. */ getParseStatus(): GetParseStatusOperation { return new GetParseStatusOperation(this); }

  /** Send one bounded parse-status request. */
  async readStatus(call: Call, uid: string): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let apiKey = '';
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      apiKey = credentials.apiKey instanceof SecretString ? credentials.apiKey.reveal() : '';
    } catch { return { error: 'credential', retryAfterMs: 0 }; }
    if (!apiKey || apiKey.length > 1024 || /[^\x21-\x7e]/.test(apiKey)) return { error: 'credential', retryAfterMs: 0 };
    const url = endpoint + '?' + new URLSearchParams({ uid }).toString();
    const request: ProviderRequest = { url, method: 'GET', headers: { Authorization: 'Bearer ' + apiKey, Accept: 'application/json' }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')) };
  }
}

/** Read the current state and progress of an existing PDF task. */
export class GetParseStatusOperation implements Query<GetParseStatusInput, GetParseStatusOutput> {
  /** Bind the provider client. */ constructor(private readonly client: Doc2xClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return GetParseStatusDefinition; }
  /** Classify one status request according to the Go connector contract. */
  async invoke(call: Call, input: GetParseStatusInput): Promise<QueryAttempt<GetParseStatusOutput>> {
    const uid = input?.uid;
    if (typeof uid !== 'string' || !uid || uid.length > 256 || uid.trim() !== uid || Array.from(uid).some(character => character.charCodeAt(0) === 0 || character === '\r' || character === '\n')) return branch('defect', 'VALIDATION');
    const exchange = await this.client.readStatus(call, uid);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)); } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(wire)) return branch('invalidResponse', 'PROTOCOL');
    if (wire.code !== 'success') return branch('providerRejected', 'PROVIDER_REJECTION');
    if (!isRecord(wire.data) || !['processing', 'failed', 'success'].includes(String(wire.data.status))) return branch('invalidResponse', 'PROTOCOL');
    const progress = wire.data.progress ?? 0;
    if (!Number.isInteger(progress) || (progress as number) < 0 || (progress as number) > 100) return branch('invalidResponse', 'PROTOCOL');
    return newQueryBranch('found', { status: wire.data.status as string, progress: progress as number }, {});
  }
}

function isRecord(value: unknown): value is Record<string, unknown> { return value !== null && typeof value === 'object' && !Array.isArray(value); }
function header(headers: Record<string, string | string[] | undefined>, name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'doc2x', operation: 'getParseStatus', kind, message: 'Doc2X parse-status query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<GetParseStatusOutput> { return newQueryBranch(name, emptyOutput, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<GetParseStatusOutput> { return newQueryRetry(failure(kind), delay); }
