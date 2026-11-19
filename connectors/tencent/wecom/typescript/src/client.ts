// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { BodyTooLargeError, ProviderTransportError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import type { Call, CredentialProvider, Failure } from '@superdurable/connectors-sdk';
import type { Config, Credentials } from './generated/models.js';
import { SendApplicationMessageOperation, SendGroupRobotMessageOperation } from './operations.js';

/** Injectable provider sender for deterministic recordings. */
export type WeComRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;
/** Optional transport and clock dependencies. */
export interface WeComClientOptions {
  /** HTTP sender; defaults to the SDK's bounded transport. */ send?: WeComRequestSender;
  /** Clock used for token expiry and the duplicate window. */ now?: () => Date;
}
/** One provider exchange with response completeness evidence. */
export interface WeComExchange {
  /** HTTP status, or zero when no response arrived. */ status: number;
  /** Complete bounded body. */ body: Uint8Array;
  /** Provider headers. */ headers: ProviderResponse['headers'];
  /** Broken read, oversized body, or transport failure. */ error?: 'read' | 'tooLarge' | 'request';
  /** Transport proved the request did not leave the process. */ isProvablyNotSent?: boolean;
}
/** Classified access-token outcome. */
export type AccessTokenOutcome = { token: string } | { failure: Failure; disposition: 'retry' | 'rejected' };

/** WeCom client with an in-memory application token cache. */
export class WeComClient {
  private readonly endpoint: string;
  private readonly maxResponseBytes: number;
  private readonly timeoutMs: number;
  private readonly tokenCache = new Map<string, { token: string; expiresAt: number }>();
  private readonly pendingTokens = new Map<string, Promise<AccessTokenOutcome>>();
  readonly credentials: CredentialProvider<Credentials>;
  private readonly send: WeComRequestSender;
  readonly now: () => Date;

  /** Validate settings and bind dependencies without contacting WeCom. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: WeComClientOptions = {}) {
    if (!credentials) throw new TypeError('WeCom credential source is required');
    this.endpoint = validateBaseURL(config.baseUrl || 'https://qyapi.weixin.qq.com');
    this.maxResponseBytes = config.maxResponseBytes ?? 262_144;
    this.timeoutMs = config.timeoutMs ?? 10_000;
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || !Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0) throw new TypeError('WeCom timeout and maxResponseBytes must be positive');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }
  /** Bind the group robot Mutation. */ sendGroupRobotMessage(): SendGroupRobotMessageOperation { return new SendGroupRobotMessageOperation(this); }
  /** Bind the application Mutation. */ sendApplicationMessage(): SendApplicationMessageOperation { return new SendApplicationMessageOperation(this); }

  /** Send once without exposing a secret-bearing URL or transport text. */
  async exchange(method: 'GET' | 'POST', path: string, query: URLSearchParams, body: Uint8Array | undefined, signal: AbortSignal): Promise<WeComExchange> {
    try {
      const response = await this.send({ url: `${this.endpoint}${path}?${query.toString()}`, method,
        headers: { Accept: 'application/json', ...(body ? { 'Content-Type': 'application/json' } : {}) },
        ...(body ? { body } : {}), maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
        addressPolicy: this.endpoint === 'https://qyapi.weixin.qq.com' ? 'publicHTTPS' : 'privateNetworkHTTP', signal });
      const needsBody = (response.status >= 200 && response.status < 300) || refusalStatuses.has(response.status);
      if (needsBody && response.body.length > this.maxResponseBytes) return { status: response.status, headers: response.headers, body: new Uint8Array(), error: 'tooLarge' };
      return { status: response.status, headers: response.headers, body: needsBody ? response.body : new Uint8Array() };
    } catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { status: error.status, headers: error.headers ?? {}, body: new Uint8Array(), error: 'tooLarge' };
      if (error instanceof ProviderTransportError && error.status !== undefined && error.status >= 100)
        return { status: error.status, headers: error.headers ?? {}, body: new Uint8Array(), error: 'read' };
      return { status: 0, headers: {}, body: new Uint8Array(), error: 'request', isProvablyNotSent: method === 'POST' && this.send === sendProviderRequest && error instanceof ProviderTransportError && error.isProvablyNotSent };
    }
  }

  /** Resolve or fetch one token, keyed by corp ID and secret fingerprint. */
  async accessToken(call: Call, corpId: string, corpSecret: string, operation: string): Promise<AccessTokenOutcome> {
    const key = this.tokenKey(corpId, corpSecret);
    const cached = this.tokenCache.get(key);
    if (cached && this.now().getTime() < cached.expiresAt) return { token: cached.token };
    const pending = this.pendingTokens.get(key);
    if (pending) return pending;
    const fetching = this.fetchToken(call, corpId, corpSecret, operation).then(outcome => {
      if ('token' in outcome) this.tokenCache.set(key, { token: outcome.token, expiresAt: this.now().getTime() + outcome.lifetimeMs });
      return 'token' in outcome ? { token: outcome.token } as AccessTokenOutcome : outcome;
    }).finally(() => this.pendingTokens.delete(key));
    this.pendingTokens.set(key, fetching);
    return fetching;
  }
  /** Discard a rejected token without evicting a newer replacement. */
  invalidateAccessToken(corpId: string, corpSecret: string, token: string): void {
    const key = this.tokenKey(corpId, corpSecret);
    if (this.tokenCache.get(key)?.token === token) this.tokenCache.delete(key);
  }
  private tokenKey(corpId: string, secret: string): string { return `${corpId}:${createHash('sha256').update(secret).digest('hex')}`; }
  private async fetchToken(call: Call, corpId: string, corpSecret: string, operation: string): Promise<({ token: string; lifetimeMs: number } | { failure: Failure; disposition: 'retry' | 'rejected' })> {
    const failure = (kind: Failure['kind'], message: string): Failure => ({ kind, provider: 'wecom', operation, message });
    const exchange = await this.exchange('GET', '/cgi-bin/gettoken', new URLSearchParams({ corpid: corpId, corpsecret: corpSecret }), undefined, call.context.cancellationSignal);
    if (!exchange.status) return { disposition: 'retry', failure: failure('TRANSPORT', 'WeCom access token request failed') };
    if (exchange.status === 429) return { disposition: 'retry', failure: failure('RATE_LIMIT', 'WeCom rate limited the access token request') };
    if (exchange.status === 408 || exchange.status >= 500) return { disposition: 'retry', failure: failure('AVAILABILITY', 'WeCom access token service is unavailable') };
    if (refusalStatuses.has(exchange.status)) return { disposition: 'rejected', failure: failure('PROVIDER_REJECTION', 'WeCom rejected the access token request with an unexpected HTTP status') };
    if (exchange.status !== 200) return { disposition: 'retry', failure: failure('AVAILABILITY', 'WeCom access token outcome is unknown after an unexpected HTTP status') };
    if (exchange.error === 'tooLarge') return { disposition: 'retry', failure: failure('RESPONSE_TOO_LARGE', 'WeCom access token response exceeded maxResponseBytes') };
    if (exchange.error) return { disposition: 'retry', failure: failure('TRANSPORT', 'WeCom access token response was interrupted') };
    const envelope = decodeEnvelope(exchange.body);
    if (!envelope) return { disposition: 'retry', failure: failure('PROTOCOL', 'WeCom access token response is malformed') };
    if (isRetryableCode(envelope.errcode)) return { disposition: 'retry', failure: failure(kindForCode(envelope.errcode), `WeCom deferred the access token request with errcode ${envelope.errcode}`) };
    if (envelope.errcode !== 0) return { disposition: 'rejected', failure: failure(kindForCode(envelope.errcode), `WeCom rejected the application credentials with errcode ${envelope.errcode}`) };
    if (typeof envelope.access_token !== 'string' || !envelope.access_token || Buffer.byteLength(envelope.access_token) > 512 || !isHeaderSafeCredential(envelope.access_token) || typeof envelope.expires_in !== 'number' || !Number.isSafeInteger(envelope.expires_in) || envelope.expires_in <= 0)
      return { disposition: 'retry', failure: failure('PROTOCOL', 'WeCom access token response is incomplete') };
    const lifetimeMs = envelope.expires_in > 600 ? (envelope.expires_in - 300) * 1000 : envelope.expires_in * 500;
    return { token: envelope.access_token, lifetimeMs };
  }
}

/** HTTP statuses that conclusively refuse a WeCom write. */
export const refusalStatuses = new Set([400, 401, 403, 404, 411, 413, 414, 415, 422]);
/** Decode numeric WeCom errcode without provider-controlled error text. */
export function decodeEnvelope(body: Uint8Array): Record<string, unknown> & { errcode: number } | undefined {
  try { const value: unknown = JSON.parse(Buffer.from(body).toString('utf8'));
    return value && typeof value === 'object' && !Array.isArray(value) && typeof (value as Record<string, unknown>).errcode === 'number' && Number.isInteger((value as Record<string, number>).errcode)
      ? value as Record<string, unknown> & { errcode: number } : undefined;
  } catch { return undefined; }
}
/** Map WeCom codes to stable failure kinds. */
export function kindForCode(code: number): Failure['kind'] {
  if ([40001, 40013, 40014, 41001, 42001].includes(code)) return 'AUTHENTICATION';
  if ([60020, 48002, 301002].includes(code)) return 'AUTHORIZATION';
  if ([45009, 45033].includes(code)) return 'RATE_LIMIT';
  if (code === -1) return 'AVAILABILITY';
  return 'PROVIDER_REJECTION';
}
/** Whether WeCom explicitly deferred a request without processing it. */
export function isRetryableCode(code: number): boolean { return [-1, 45009, 45033].includes(code); }
/** Parse a provider Retry-After header using the injectable clock. */
export function retryAfterMs(exchange: WeComExchange, now: Date): number {
  const raw = exchange.headers['retry-after'] ?? exchange.headers['Retry-After'];
  return parseRetryAfter(Array.isArray(raw) ? raw[0] ?? '' : raw ?? '', now);
}
/** A readable refusal body cannot report acceptance. */
export function isReadableRefusal(exchange: WeComExchange): boolean {
  if (exchange.error) return false;
  try { const value: unknown = JSON.parse(Buffer.from(exchange.body).toString('utf8'));
    if (!value || typeof value !== 'object' || Array.isArray(value)) return true;
    const record = value as Record<string, unknown>;
    return (typeof record.errcode !== 'number' || record.errcode !== 0) && (typeof record.msgid !== 'string' || record.msgid === '');
  } catch { return true; }
}
