// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { parseBalanceResponse, integerCode } from './decimal-response.js';
import { randomBytes } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetAccountBalanceDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetAccountBalanceInput, GetAccountBalanceOutput } from './generated/models.js';

/** Test seams for the provider transport and nonce source. */
export interface FanyigouClientOptions { send?: (request: ProviderRequest) => Promise<ProviderResponse>; nonce?: () => Uint8Array; }
const emptyBalance: GetAccountBalanceOutput = { deductType: 0, balance: 0 };

/** Read a Fanyigou document balance using each call's credentials. */
export class FanyigouClient {
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly nonce: () => Uint8Array;
  /** Validate local request limits without contacting Fanyigou. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: FanyigouClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Fanyigou configuration and credentials are required');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > 16 << 20) throw new TypeError('Fanyigou limits are invalid');
    this.send = options.send ?? sendProviderRequest;
    this.nonce = options.nonce ?? (() => randomBytes(16));
  }
  /** Bind the read-only account balance Query. */ getAccountBalance(): GetAccountBalanceOperation { return new GetAccountBalanceOperation(this); }
  /** Send one authenticated balance request. */
  async fetchBalance(call: Call): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let appID = '', privateKey = '';
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      appID = credentials.appID instanceof SecretString ? credentials.appID.reveal() : '';
      privateKey = credentials.privateKey instanceof SecretString ? credentials.privateKey.reveal() : '';
    } catch { return { error: 'credential', retryAfterMs: 0 }; }
    if (!validCredential(appID) || !validCredential(privateKey)) return { error: 'credential', retryAfterMs: 0 };
    let nonce: Uint8Array;
    try { nonce = this.nonce(); } catch { return { error: 'transport', retryAfterMs: 0 }; }
    if (nonce.length !== 16) return { error: 'transport', retryAfterMs: 0 };
    const form = new URLSearchParams({ appid: appID, nonce_str: Buffer.from(nonce).toString('hex'), type: '1' });
    const request: ProviderRequest = { url: 'https://www.fanyigou.com/TranslateApi/api/getAccount', method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded; charset=UTF-8', Accept: 'application/json', Authorization: 'Bearer ' + privateKey }, body: new TextEncoder().encode(form.toString()), timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')) };
  }
}

/** Classify Fanyigou account responses with the Go Query contract. */
export class GetAccountBalanceOperation implements Query<GetAccountBalanceInput, GetAccountBalanceOutput> {
  /** Bind the provider client. */ constructor(private readonly client: FanyigouClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return GetAccountBalanceDefinition; }
  /** Invoke one balance read. */
  async invoke(call: Call): Promise<QueryAttempt<GetAccountBalanceOutput>> {
    const exchange = await this.client.fetchBalance(call);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (status !== 200) return branch('providerRejected', 'PROVIDER_REJECTION');
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    let wire: unknown;
    try { wire = parseBalanceResponse(body); } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isObject(wire)) return branch('invalidResponse', 'PROTOCOL');
    const code=integerCode(wire.code);
    const data=wire.data;
    if (code===undefined || (data!=null && !isObject(data))) return branch('invalidResponse','PROTOCOL');
    const units=integerCode(data?.deducttype);
    const balance=integerCode(data?.balance);
    if ((data?.deducttype!=null && units===undefined) || (data?.balance!=null && balance===undefined)) return branch('invalidResponse','PROTOCOL');
    if (code===102n || code===105n) return retry('AVAILABILITY',exchange.retryAfterMs);
    if (code===108n) return retry('RATE_LIMIT',exchange.retryAfterMs);
    if (code!==100n) return branch('providerRejected','PROVIDER_REJECTION');
    if ((units!==0n && units!==1n) || balance===undefined || balance<0n || balance>BigInt(Number.MAX_SAFE_INTEGER)) return branch('invalidResponse','PROTOCOL');
    return newQueryBranch('found',{deductType:Number(units),balance:Number(balance)});
  }
}
function validCredential(value: string): boolean { return !!value && Buffer.byteLength(value) <= 1024 && value.trim() === value && ![...value].some(character => character.charCodeAt(0) < 32 || character.charCodeAt(0) === 127); }
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && Object.getPrototypeOf(value) === Object.prototype; }
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'fanyigou', operation: 'getAccountBalance', kind, message: 'Fanyigou account balance query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<GetAccountBalanceOutput> { return newQueryBranch(name, emptyBalance, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<GetAccountBalanceOutput> { return newQueryRetry(failure(kind), delay); }
