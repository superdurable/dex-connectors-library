// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHmac } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetSignFlowStatusDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetSignFlowStatusInput, GetSignFlowStatusOutput } from './generated/models.js';

/** Optional sender and clock for replaying signed provider requests. */
export interface EsignClientOptions {
  /** Sender; omitted uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Clock; omitted uses wall time. */ now?: () => Date;
}

/** Query eSign signing status without exposing signer or document fields. */
export class EsignClient {
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;

  /** Validate connection settings without contacting eSign. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: EsignClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('eSign configuration and credentials are required');
    this.timeoutMs = config.timeoutMs || 10_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('eSign request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the status Query. */ getSignFlowStatus(): GetSignFlowStatusOperation { return new GetSignFlowStatusOperation(this); }

  /** Sign and send one bounded request. */
  async read(call: Call, input: GetSignFlowStatusInput): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let appID = '', secret = '';
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      appID = credentials.appId;
      secret = credentials.appSecret instanceof SecretString ? credentials.appSecret.reveal() : '';
    } catch { return { error: 'credential', retryAfterMs: 0 }; }
    if (!validCredential(appID) || !validCredential(secret)) return { error: 'credential', retryAfterMs: 0 };
    const path = `/v3/sign-flow/${input.signFlowId}/detail`;
    const canonical = `GET\n*/*\n\napplication/json; charset=UTF-8\n\n${path}`;
    const request: ProviderRequest = { url: `https://openapi.esign.cn${path}`, method: 'GET', headers: { Accept: '*/*', 'Content-Type': 'application/json; charset=UTF-8', 'X-Tsign-Open-App-Id': appID, 'X-Tsign-Open-Auth-Mode': 'Signature', 'X-Tsign-Open-Ca-Timestamp': String(this.now().getTime()), 'X-Tsign-Open-Ca-Signature': createHmac('sha256', secret).update(canonical).digest('base64') }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')) };
  }
}

/** Read one signing flow's numeric status. */
export class GetSignFlowStatusOperation implements Query<GetSignFlowStatusInput, GetSignFlowStatusOutput> {
  /** Bind a validated eSign client. */ constructor(private readonly client: EsignClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return GetSignFlowStatusDefinition; }
  /** Classify signed request and provider outcomes. */
  async invoke(call: Call, input: GetSignFlowStatusInput): Promise<QueryAttempt<GetSignFlowStatusOutput>> {
    if (typeof input?.signFlowId !== 'string' || !/^[a-zA-Z0-9_-]{1,128}$/.test(input.signFlowId)) return branch('defect', 'VALIDATION');
    const exchange = await this.client.read(call, input);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isObject(wire) || !Number.isInteger(wire.code)) return branch('invalidResponse', 'PROTOCOL');
    if (wire.code !== 0) return branch('providerRejected', 'PROVIDER_REJECTION');
    if (!isObject(wire.data) || !Number.isInteger(wire.data.signFlowStatus) || (wire.data.signFlowStatus as number) < 0) return branch('invalidResponse', 'PROTOCOL');
    return newQueryBranch('found', { signFlowId: input.signFlowId, signFlowStatus: wire.data.signFlowStatus as number });
  }
}

function validCredential(value: unknown): value is string { return typeof value === 'string' && value.length > 0 && value.length <= 1024 && /^[\x21-\x7e]+$/.test(value); }
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'esign', operation: 'getSignFlowStatus', kind, message: 'eSign status query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<GetSignFlowStatusOutput> { return newQueryBranch(name, { signFlowId: '', signFlowStatus: 0 }, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<GetSignFlowStatusOutput> { return newQueryRetry(failure(kind), delay); }
