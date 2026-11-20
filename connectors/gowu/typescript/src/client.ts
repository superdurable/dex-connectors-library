// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash, createHmac, randomBytes } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetAccessClientDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetAccessClientInput, AccessClientResult } from './generated/models.js';

const empty: AccessClientResult = { remark: '', apiCallCount: 0 };
/** Inject transport, timestamp, and nonce for deterministic verification. */
export interface GowuClientOptions {
  /** Dispatch requests; production uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Supply signing time; production uses the current clock. */ now?: () => Date;
  /** Supply 16 nonce bytes; production uses cryptographic randomness. */ nonce?: () => Uint8Array;
}
/** Read authorized Gowu application metadata without exposing credentials. */
export class GowuClient {
  readonly baseUrl: string;
  readonly timeoutMs: number;
  readonly maxResponseBytes: number;
  readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  readonly now: () => Date;
  readonly nonce: () => Uint8Array;
  /** Validate configuration without contacting Gowu. */
  constructor(config: Config, readonly credentials: CredentialProvider<Credentials>, options: GowuClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Gowu configuration and credential source are required');
    const url = new URL(config.baseUrl ?? 'https://api.gowu.net');
    if (url.protocol !== 'https:' || url.username || url.password || url.search || url.hash) throw new TypeError('Gowu base URL is invalid');
    this.baseUrl = url.origin;
    this.timeoutMs = config.timeoutMs ?? 15_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 262_144;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > 16 << 20) throw new TypeError('Gowu request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
    this.nonce = options.nonce ?? (() => randomBytes(16));
  }
  /** Bind the application information Query. */ getAccessClient(): GetAccessClientOperation { return new GetAccessClientOperation(this); }
}
/** Validate signed application responses and classify provider outcomes. */
export class GetAccessClientOperation implements Query<GetAccessClientInput, AccessClientResult> {
  /** Bind a validated client. */ constructor(private readonly client: GowuClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return GetAccessClientDefinition; }
  /** Read one application response. */
  async invoke(call: Call, input: GetAccessClientInput): Promise<QueryAttempt<AccessClientResult>> {
    void input;
    let credentials: Credentials;
    try { credentials = await this.client.credentials.resolve(call, call.context.cancellationSignal); } catch { return branch('defect', 'AUTHENTICATION'); }
    const key = credentials.appKey;
    const secret = credentials.appSecret instanceof SecretString ? credentials.appSecret.reveal() : '';
    if (typeof key !== 'string' || !key || key.length > 128 || key.trim() !== key || !secret || secret.length > 2048 || secret.trim() !== secret) return branch('defect', 'AUTHENTICATION');
    let nonce: string;
    try { const bytes = this.client.nonce(); if (bytes.length !== 16) throw new TypeError('nonce length'); nonce = Buffer.from(bytes).toString('hex'); } catch { return retry('AVAILABILITY'); }
    const timestamp = String(Math.floor(this.client.now().getTime() / 1000));
    const emptyBodyHash = createHash('sha256').update('').digest('hex');
    const canonical = ['OCP-SIGN-V2', timestamp, nonce, 'GET', '/v1/access-clients/me', '', emptyBodyHash, ''].join('\n');
    const signature = createHmac('sha256', secret).update(canonical).digest('hex');
    const request: ProviderRequest = { url: `${this.client.baseUrl}/v1/access-clients/me`, method: 'GET', headers: { Accept: 'application/json', 'x-app-key': key, 'x-timestamp': timestamp, 'x-nonce': nonce, 'x-sign-version': '2', 'x-signature': signature }, timeoutMs: this.client.timeoutMs, maxResponseBytes: this.client.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.client.send(request); } catch (error) { return error instanceof BodyTooLargeError && error.status !== undefined ? branch('invalidResponse', 'RESPONSE_TOO_LARGE') : retry('AVAILABILITY'); }
    const header = Object.entries(response.headers).find(([name]) => name.toLowerCase() === 'retry-after')?.[1];
    const delay = parseRetryAfter(Array.isArray(header) ? header[0] ?? '' : header ?? '');
    if (response.status === 429) return retry('RATE_LIMIT', delay);
    if (response.status === 408 || response.status >= 500) return retry('AVAILABILITY', delay);
    if (response.body.length > this.client.maxResponseBytes) return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (response.status !== 200) return branch('providerRejected', response.status === 401 ? 'AUTHENTICATION' : response.status === 403 ? 'AUTHORIZATION' : [400, 404, 412].includes(response.status) ? 'VALIDATION' : 'PROVIDER_REJECTION');
    let document: unknown;
    try { document = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(response.body)) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(document) || document.appKey !== key || !Number.isSafeInteger(document.apiCallCount) || (document.apiCallCount as number) < 0 || (document.remark !== undefined && (typeof document.remark !== 'string' || document.remark.length > 256))) return branch('invalidResponse', 'PROTOCOL');
    return newQueryBranch('found', { remark: typeof document.remark === 'string' ? document.remark : '', apiCallCount: document.apiCallCount as number });
  }
}
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function failure(kind: Failure['kind']): Failure { return { provider: 'gowu', operation: 'getAccessClient', kind, message: 'Gowu application query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<AccessClientResult> { return newQueryBranch(name, empty, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<AccessClientResult> { return newQueryRetry(failure(kind), delay); }
