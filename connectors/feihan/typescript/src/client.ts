// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { QueryRechargeOrderDefinition } from './generated/definitions.js';
import type { Config, Credentials, QueryRechargeOrderInput, RechargeOrderResult } from './generated/models.js';

/** Optional HTTP transport for local provider tests. */
export interface FeihanClientOptions { send?: (request: ProviderRequest) => Promise<ProviderResponse>; }
const emptyOrder: RechargeOrderResult = { merchantOrderId: '', rechargeStatus: 0 };

/** Resolve Feihan project credentials for each order query. */
export class FeihanClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate connection settings without contacting Feihan. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: FeihanClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Feihan configuration and credentials are required');
    const baseUrl = config.baseUrl || 'https://api2.ofpay.com';
    const origin = new URL(baseUrl);
    if (origin.protocol !== 'https:' || !origin.hostname || origin.username || origin.password || origin.search || origin.hash) throw new TypeError('Feihan HTTPS origin is invalid');
    this.baseUrl = baseUrl.replace(/\/$/, '');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 262_144;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('Feihan request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the read-only order status Query. */ queryRechargeOrder(): QueryRechargeOrderOperation { return new QueryRechargeOrderOperation(this); }

  /** Sign and send one bounded order query. */
  async fetchOrder(call: Call, orderId: string): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let credentials: Credentials;
    try { credentials = await this.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return { error: 'credential', retryAfterMs: 0 }; }
    const projectId = credentials.projectId;
    const password = credentials.password instanceof SecretString ? credentials.password.reveal() : '';
    const keyStr = credentials.keyStr instanceof SecretString ? credentials.keyStr.reveal() : '';
    if (typeof projectId !== 'string' || !/^A[A-Z0-9]{1,31}$/.test(projectId) || !validCredential(password) || !validCredential(keyStr)) return { error: 'credential', retryAfterMs: 0 };
    const passwordHash = createHash('md5').update(password).digest('hex');
    const signature = createHash('md5').update(projectId + passwordHash + orderId + keyStr).digest('hex').toUpperCase();
    const form = new URLSearchParams({ format: 'json', md5_str: signature, sporder_id: orderId, userid: projectId, userpws: passwordHash, version: '6.0' });
    const request: ProviderRequest = { url: this.baseUrl + '/queryOrderInfo.do', method: 'POST', headers: { Accept: 'application/json', 'Content-Type': 'application/x-www-form-urlencoded' }, body: new TextEncoder().encode(form.toString()), timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')) };
  }
}

/** Query the status of one known Feihan recharge order. */
export class QueryRechargeOrderOperation implements Query<QueryRechargeOrderInput, RechargeOrderResult> {
  /** Bind the provider client. */ constructor(private readonly client: FeihanClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return QueryRechargeOrderDefinition; }
  /** Classify one provider response with the Go connector contract. */
  async invoke(call: Call, input: QueryRechargeOrderInput): Promise<QueryAttempt<RechargeOrderResult>> {
    const orderId = input?.merchantOrderId;
    if (typeof orderId !== 'string' || !/^[!-~]{1,64}$/.test(orderId)) return branch('defect', 'VALIDATION');
    const exchange = await this.client.fetchOrder(call, orderId);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : status === 400 || status === 404 || status === 412 ? 'VALIDATION' : 'PROVIDER_REJECTION');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!wire || typeof wire !== 'object' || Array.isArray(wire)) return branch('invalidResponse', 'PROTOCOL');
    const order = wire as Record<string, unknown>;
    if (typeof order.retcode !== 'string' || !order.retcode) return branch('invalidResponse', 'PROTOCOL');
    if (order.retcode !== '1') return branch('providerRejected', 'PROVIDER_REJECTION');
    if (order.sporder_id !== orderId || !Number.isInteger(order.game_state) || ![-1, 0, 1, 9].includes(order.game_state as number)) return branch('invalidResponse', 'PROTOCOL');
    if (order.game_state === -1) return branch('providerRejected', 'PROVIDER_REJECTION');
    return newQueryBranch('found', { merchantOrderId: orderId, rechargeStatus: order.game_state as number });
  }
}

function validCredential(value: string): boolean { return !!value && value.length <= 2048 && value.trim() === value; }
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'feihan', operation: 'queryRechargeOrder', kind, message: 'Feihan recharge order query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<RechargeOrderResult> { return newQueryBranch(name, emptyOrder, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<RechargeOrderResult> { return newQueryRetry(failure(kind), delay); }
