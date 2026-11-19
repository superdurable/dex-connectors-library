// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash, createHmac, randomBytes } from 'node:crypto';
import { isIP } from 'node:net';
import { newQueryBranch, newQueryRetry, SecretString, zeroReceipt, type Call, type Failure, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetAuthorizedUserDefinition } from './generated/definitions.js';
import type { AuthorizedUser, Config, Credentials, CredentialSource, GetAuthorizedUserInput } from './generated/models.js';

/** Dependencies for deterministic provider tests and approved HTTP transports. */
export interface BilibiliClientOptions {
  /** Provider sender; the SDK sender is used by default. */
  send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Signing clock in seconds; the system clock is used by default. */
  now?: () => Date;
  /** Sixteen cryptographically random signing bytes; randomBytes is used by default. */
  nonce?: () => Uint8Array;
}

/** Bounded Bilibili Open Platform client with per-call credentials. */
export class BilibiliClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;
  private readonly nonce: () => Uint8Array;

  /** Validate the API origin and limits without contacting Bilibili. */
  constructor(config: Config, private readonly credentials: CredentialSource, options: BilibiliClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Bilibili configuration and credential source are required');
    const url = new URL(config.baseUrl ?? 'https://member.bilibili.com');
    const hostname = url.hostname.toLowerCase();
    const isLoopback = hostname === 'localhost' || (isIP(hostname) === 4 && hostname.startsWith('127.')) || hostname === '[::1]';
    if ((url.protocol !== 'https:' && !(url.protocol === 'http:' && isLoopback)) || url.username || url.password || url.search || url.hash || url.pathname !== '/')
      throw new TypeError('Bilibili API URL must be an HTTPS origin or loopback test origin');
    this.baseURL = url.origin;
    this.timeoutMs = config.timeoutMs ?? 15_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 32_768;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('Bilibili request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
    this.nonce = options.nonce ?? (() => randomBytes(16));
  }

  /** Bind the authorized user Query. */
  getAuthorizedUser(): GetAuthorizedUserOperation { return new GetAuthorizedUserOperation(this); }

  /** Resolve credentials, sign, and send one bounded account request. */
  async requestAuthorizedUser(call: Call): Promise<{ response?: ProviderResponse; tooLarge?: boolean; failure?: Failure; retry?: boolean }> {
    let credentials: Credentials;
    try { credentials = await this.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return { failure: failure('AUTHENTICATION', 'Bilibili credentials are unavailable or invalid') }; }
    const clientID = credentials.clientID;
    const appSecret = credentials.appSecret instanceof SecretString ? credentials.appSecret.reveal() : '';
    const accessToken = credentials.accessToken instanceof SecretString ? credentials.accessToken.reveal() : '';
    if (![clientID, appSecret, accessToken].every(isOpaqueCredential))
      return { failure: failure('AUTHENTICATION', 'Bilibili credentials are unavailable or invalid') };
    let nonce: Uint8Array;
    try { nonce = this.nonce(); }
    catch { return { failure: failure('TRANSPORT', 'Bilibili request failed'), retry: true }; }
    if (nonce.length !== 16) return { failure: failure('TRANSPORT', 'Bilibili request failed'), retry: true };
    const signedHeaders: Record<string, string> = {
      'x-bili-accesskeyid': clientID,
      'x-bili-content-md5': createHash('md5').update('').digest('hex'),
      'x-bili-signature-method': 'HMAC-SHA256',
      'x-bili-signature-nonce': Buffer.from(nonce).toString('hex'),
      'x-bili-signature-version': '2.0',
      'x-bili-timestamp': Math.floor(this.now().getTime() / 1000).toString(),
    };
    const signature = createHmac('sha256', appSecret).update(Object.entries(signedHeaders).map(([key, value]) => `${key}:${value}`).join('\n')).digest('hex');
    const request: ProviderRequest = {
      url: `${this.baseURL}/arcopen/fn/user/account/info`, method: 'GET',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json', 'Access-Token': accessToken, ...signedHeaders, Authorization: signature },
      timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes,
      addressPolicy: this.baseURL.startsWith('https:') ? 'publicHTTPS' : 'privateNetworkHTTP',
      signal: call.context.cancellationSignal,
    };
    try { return { response: await this.send(request) }; }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined)
        return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, tooLarge: true };
      return { failure: failure('TRANSPORT', 'Bilibili request failed'), retry: true };
    }
  }
}

/** Query the authorized Bilibili account's public identity. */
export class GetAuthorizedUserOperation implements Query<GetAuthorizedUserInput, AuthorizedUser> {
  /** Bind the client. */ constructor(private readonly client: BilibiliClient) {}
  /** Return the generated Query definition. */ definition() { return GetAuthorizedUserDefinition; }
  /** Classify one provider attempt into a durable Query decision. */
  async invoke(call: Call, input: GetAuthorizedUserInput): Promise<QueryAttempt<AuthorizedUser>> {
    void input;
    const result = await this.client.requestAuthorizedUser(call);
    if (result.failure) return result.retry ? newQueryRetry(result.failure) : terminal('defect', result.failure.kind, result.failure.message);
    const response = result.response;
    if (!response) return newQueryRetry(failure('TRANSPORT', 'Bilibili request failed'));
    const retryAfter = retryDelay(response);
    if (response.status === 429) return newQueryRetry(failure('RATE_LIMIT', 'Bilibili rate limited the query'), retryAfter);
    if (response.status === 408 || response.status >= 500) return newQueryRetry(failure('AVAILABILITY', 'Bilibili is temporarily unavailable'), retryAfter);
    if (result.tooLarge) return terminal('invalidResponse', 'RESPONSE_TOO_LARGE', 'Bilibili response exceeded the configured limit');
    if (response.status !== 200) return terminal('providerRejected', 'PROVIDER_REJECTION', `Bilibili rejected HTTP ${response.status}`);
    let value: unknown;
    try { value = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(response.body)); }
    catch { return terminal('invalidResponse', 'PROTOCOL', 'Bilibili returned invalid account JSON'); }
    if (!value || typeof value !== 'object' || Array.isArray(value)) return terminal('invalidResponse', 'PROTOCOL', 'Bilibili returned invalid account JSON');
    const fields = value as Record<string, unknown>;
    if (!Number.isSafeInteger(fields.code)) return terminal('invalidResponse', 'PROTOCOL', 'Bilibili returned no status code');
    const code = fields.code as number;
    if ([127009, 127306].includes(code)) return newQueryRetry(failure('RATE_LIMIT', 'Bilibili rate limited the query'), retryAfter);
    if ([4010, 4011, 122009, 122010, 123002, 127039].includes(code)) return newQueryRetry(failure('AVAILABILITY', 'Bilibili is temporarily unavailable'), retryAfter);
    if (code !== 0) return terminal('providerRejected', 'PROVIDER_REJECTION', `Bilibili rejected code ${code}`);
    if (!fields.data || typeof fields.data !== 'object' || Array.isArray(fields.data)) return terminal('invalidResponse', 'PROTOCOL', 'Bilibili returned incomplete identity');
    const identity = fields.data as Record<string, unknown>;
    if (typeof identity.openid !== 'string' || !identity.openid || typeof identity.name !== 'string' || !identity.name)
      return terminal('invalidResponse', 'PROTOCOL', 'Bilibili returned incomplete identity');
    return newQueryBranch('found', { openID: identity.openid, name: identity.name }, zeroReceipt());
  }
}

function isOpaqueCredential(value: unknown): value is string { return typeof value === 'string' && value.length > 0 && value.length <= 4096 && /^[!-~]+$/.test(value); }
function failure(kind: Failure['kind'], message: string): Failure { return { kind, provider: 'bilibili', operation: 'getAuthorizedUser', message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<AuthorizedUser> { return newQueryBranch(branch, { openID: '', name: '' }, zeroReceipt(), failure(kind, message)); }
function retryDelay(response: ProviderResponse): number {
  const value = Object.entries(response.headers).find(([name]) => name.toLowerCase() === 'retry-after')?.[1];
  return parseRetryAfter(Array.isArray(value) ? value[0] ?? '' : value ?? '') || 0;
}
