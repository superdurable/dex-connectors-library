// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { BodyTooLargeError, MaxErrorBodyBytes, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ReauthorizationRequiredError, SecretString, type Call, type CredentialProvider, type Failure, type ReceiptDraft } from '@superdurable/connectors-sdk';
import type { Config, Credentials } from './generated/models.js';
import { AddReactionOperation, CreateMessageOperation, GetChannelOperation, ListChannelMessagesOperation } from './operations.js';

const defaultEndpoint = 'https://discord.com/api/v10';
const defaultMaxResponseBytes = 4_194_304;
const documentedCodes = new Set(['0', '10003', '10008', '10014', '20028', '30010', '40001', '40005', '50001', '50006', '50008', '50013', '50035', '50083', '90001']);
const notFoundCodes = new Set(['10003', '10008', '10014']);

/** Injectable sender for deterministic provider tests. */
export type DiscordRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;
/** Optional client dependencies. */
export interface DiscordClientOptions {
  /** Sender; omit for the SDK's bounded, no redirect transport. */
  send?: DiscordRequestSender;
  /** Clock for receipts and Retry-After. */
  now?: () => Date;
}
/** Classified provider failure. */
export interface ExchangeFailure {
  /** Retry or terminal outcome. */
  disposition: 'retry' | 'notFound' | 'rejected' | 'invalidResponse' | 'defect';
  /** Secret-safe failure. */
  failure: Failure;
  /** Delay before a Retry, in milliseconds. */
  retryAfterMs: number;
  /** Provider correlation, when a request was attempted. */
  receipt: ReceiptDraft;
}
/** Successful response or classified failure. */
export type ExchangeResult = { response: ProviderResponse } | { outcome: ExchangeFailure };

/** Authenticated Discord HTTP client with immutable configuration. */
export class DiscordClient {
  private readonly endpoint: string;
  private readonly maxResponseBytes: number;
  private readonly credentials: CredentialProvider<Credentials>;
  private readonly send: DiscordRequestSender;
  private readonly now: () => Date;

  /** Validate configuration without network access. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: DiscordClientOptions = {}) {
    if (!credentials) throw new TypeError('Discord credential source is required');
    this.endpoint = validateBaseURL(config.endpoint || defaultEndpoint);
    this.maxResponseBytes = config.maxResponseBytes || defaultMaxResponseBytes;
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('Discord maxResponseBytes must be positive');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the channel query. */ getChannel(): GetChannelOperation { return new GetChannelOperation(this); }
  /** Bind the message page query. */ listChannelMessages(): ListChannelMessagesOperation { return new ListChannelMessagesOperation(this); }
  /** Bind the message creation mutation. */ createMessage(): CreateMessageOperation { return new CreateMessageOperation(this); }
  /** Bind the reaction mutation. */ addReaction(): AddReactionOperation { return new AddReactionOperation(this); }

  /** Build a nonsecret receipt. */
  receipt(call: Call, objectId: string): ReceiptDraft {
    return { callId: call.id, provider: 'discord', observedAt: this.now().toISOString(),
      ...(call.idempotencyKey ? { idempotencyKey: call.idempotencyKey } : {}),
      ...(objectId ? { providerObjectId: objectId } : {}) };
  }

  /** Resolve credentials, perform one exchange, and classify its outcome. */
  async exchange(call: Call, operation: string, method: string, path: string, objectId: string, body?: unknown): Promise<ExchangeResult> {
    let token: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      if (!(credentials.botToken instanceof SecretString)) throw new TypeError('invalid Discord bot token');
      token = credentials.botToken.reveal();
      if (!isHeaderSafeCredential(token)) throw new TypeError('invalid Discord bot token');
    } catch (error) {
      if (error instanceof ReauthorizationRequiredError) return { outcome: this.failure('rejected', operation, 'AUTHENTICATION', 'the Discord connection must be authorized again') };
      return { outcome: this.failure('defect', operation, 'AUTHENTICATION', 'Discord connection credentials are unavailable or invalid') };
    }
    let encoded: Uint8Array | undefined;
    try { if (body !== undefined) encoded = Buffer.from(JSON.stringify(body), 'utf8'); }
    catch { return { outcome: this.failure('defect', operation, 'LOCAL_DEFECT', `${operation} request could not be encoded`) }; }
    const receipt = this.receipt(call, objectId);
    let response: ProviderResponse;
    try {
      response = await this.send({ url: this.endpoint + path, method,
        headers: { Accept: 'application/json', Authorization: `Bot ${token}`, 'User-Agent': 'DiscordBot (https://github.com/superdurable/dex-connectors-library, 0.1.0)', ...(encoded ? { 'Content-Type': 'application/json' } : {}) },
        ...(encoded ? { body: encoded } : {}), maxResponseBytes: Math.max(this.maxResponseBytes, MaxErrorBodyBytes),
        addressPolicy: this.endpoint === defaultEndpoint ? 'publicHTTPS' : 'privateNetworkHTTP', timeoutMs: 25_000,
        signal: call.context.cancellationSignal });
    } catch (error) {
      if (error instanceof BodyTooLargeError) {
        if (error.status !== undefined && (error.status < 200 || error.status >= 300)) return { outcome: this.classify(operation, { status: error.status, headers: error.headers ?? {}, body: Buffer.alloc(0) }, receipt) };
        return { outcome: this.failure('invalidResponse', operation, 'RESPONSE_TOO_LARGE', 'Discord response exceeds the configured size limit', receipt) };
      }
      return { outcome: this.failure('retry', operation, 'TRANSPORT', 'Discord did not return a complete response', receipt) };
    }
    if (response.status >= 200 && response.status < 300) {
      if (response.body.length > this.maxResponseBytes) return { outcome: this.failure('invalidResponse', operation, 'RESPONSE_TOO_LARGE', 'Discord response exceeds the configured size limit', receipt) };
      return { response };
    }
    return { outcome: this.classify(operation, response, receipt) };
  }

  private classify(operation: string, response: ProviderResponse, receipt: ReceiptDraft): ExchangeFailure {
    const status = response.status;
    const code = readErrorCode(response.body);
    const detail = code ? `, code ${code}` : '';
    const retryAfter = parseRetryAfter(header(response, 'retry-after'), this.now());
    if (status === 429) return this.failure('retry', operation, 'RATE_LIMIT', 'Discord rate limited the request', receipt, rateLimitDelay(response.body, retryAfter));
    if (status === 408 || (status >= 500 && status !== 501)) return this.failure('retry', operation, 'AVAILABILITY', `Discord is temporarily unavailable (HTTP ${status}${detail})`, receipt, retryAfter);
    if (status === 401) return this.failure('rejected', operation, 'AUTHENTICATION', `Discord rejected the bot token (HTTP 401${detail})`, receipt);
    if (status === 403) return this.failure('rejected', operation, 'AUTHORIZATION', `Discord denied access: check that the bot is in the server and has the channel permissions (HTTP 403${detail})`, receipt);
    if (status === 404 && (!code || notFoundCodes.has(code))) return this.failure('notFound', operation, 'NOT_FOUND', `Discord found no such channel, message, or emoji (HTTP 404${detail})`, receipt);
    if (status === 400 || status === 413) return this.failure('rejected', operation, 'VALIDATION', `Discord rejected the request as invalid (HTTP ${status}${detail})`, receipt);
    if (status >= 300 && status < 400) return this.failure('rejected', operation, 'PROTOCOL', `Discord redirected the request (HTTP ${status})`, receipt);
    return this.failure('rejected', operation, 'PROVIDER_REJECTION', `Discord rejected the request (HTTP ${status}${detail})`, receipt);
  }

  private failure(disposition: ExchangeFailure['disposition'], operation: string, kind: Failure['kind'], message: string, receipt: ReceiptDraft = {}, retryAfterMs = 0): ExchangeFailure {
    return { disposition, failure: { kind, provider: 'discord', operation, message }, receipt, retryAfterMs };
  }
}

function header(response: ProviderResponse, name: string): string {
  const value = response.headers[name] ?? response.headers[name.toLowerCase()];
  return Array.isArray(value) ? (value[0] ?? '') : (value ?? '');
}
function readErrorCode(body: Uint8Array): string {
  if (body.length > 65_536) return '';
  try {
    const decoded: unknown = JSON.parse(Buffer.from(body).toString('utf8'));
    if (!decoded || typeof decoded !== 'object' || !('code' in decoded)) return '';
    const code = (decoded as { code: unknown }).code;
    const text = typeof code === 'number' && Number.isInteger(code) ? String(code) : typeof code === 'string' && /^-?\d+$/.test(code) ? code : '';
    return documentedCodes.has(text) ? text : '';
  } catch { return ''; }
}
function rateLimitDelay(body: Uint8Array, fallback: number): number {
  try {
    const decoded: unknown = JSON.parse(Buffer.from(body).toString('utf8'));
    if (decoded && typeof decoded === 'object' && 'retry_after' in decoded) {
      const seconds = (decoded as { retry_after: unknown }).retry_after;
      if (typeof seconds === 'number' && Number.isFinite(seconds) && seconds > 0) return Math.ceil(Math.min(seconds, 3_600) * 1000);
    }
  } catch { /* A malformed body leaves the header delay authoritative. */ }
  return fallback;
}
