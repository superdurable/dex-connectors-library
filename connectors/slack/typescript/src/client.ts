// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { BodyTooLargeError, ProviderTransportError, isHeaderSafeCredential, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ManifestCredentialRefreshDriver } from '@superdurable/connectors-sdk/oauthtoken';
import { ReauthorizationRequiredError, resolveCredential, resolveCredentialAfterRejection, type Call, type Failure, type ReceiptDraft } from '@superdurable/connectors-sdk';
import { Authorization, CredentialFields } from './generated/refresh.js';
import type { Config, CredentialSource, Credentials } from './generated/models.js';
import { GetThreadReplyOperation, ListThreadMessagesOperation, PostChannelMessageOperation, PostThreadReplyOperation } from './operations.js';

/** One Slack request exchange, including a status when body decoding failed. */
export interface SlackExchange {
  status: number;
  headers: ProviderResponse['headers'];
  body: unknown;
  error?: 'transport' | 'notSent' | 'tooLarge' | 'invalid';
}

/** Override the provider sender and receipt clock for local tests. */
export interface SlackClientOptions {
  send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  now?: () => Date;
}

/** Slack Web API client with host-owned renewable bot and user credentials. */
export class SlackClient {
  readonly endpoint: string;
  readonly maxResponseBytes: number;
  readonly maxMessageCharacters: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;
  private readonly refreshDriver = new ManifestCredentialRefreshDriver<Credentials>({ providerName: 'Slack', auth: Authorization, credentialFields: CredentialFields });

  /** Validate configuration and bind a host credential source. */
  constructor(config: Config, readonly credentials: CredentialSource, options: SlackClientOptions = {}) {
    if (!credentials) throw new TypeError('Slack credential source is required');
    const endpoint = config.endpoint || 'https://slack.com/api';
    let parsed: URL;
    try { parsed = new URL(endpoint); } catch { throw new TypeError('Slack endpoint must be absolute'); }
    if (!parsed.hostname || (parsed.protocol !== 'https:' && parsed.hostname !== 'localhost' && parsed.hostname !== '127.0.0.1')) throw new TypeError('Slack endpoint must use HTTPS');
    this.endpoint = endpoint.replace(/\/+$/, '');
    this.maxResponseBytes = config.maxResponseBytes || 4_194_304;
    this.maxMessageCharacters = config.maxMessageCharacters || 40_000;
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes < 1 || !Number.isSafeInteger(this.maxMessageCharacters) || this.maxMessageCharacters < 1) throw new TypeError('Slack limits must be positive integers');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the list thread messages Query. */ listThreadMessages(): ListThreadMessagesOperation { return new ListThreadMessagesOperation(this); }
  /** Bind the get thread reply Query. */ getThreadReply(): GetThreadReplyOperation { return new GetThreadReplyOperation(this); }
  /** Bind the post channel message Mutation. */ postChannelMessage(): PostChannelMessageOperation { return new PostChannelMessageOperation(this); }
  /** Bind the post thread reply Mutation. */ postThreadReply(): PostThreadReplyOperation { return new PostThreadReplyOperation(this); }

  /** Resolve the required bot or user token and classify missing credentials. */
  async resolveToken(call: Call, kind: 'bot' | 'user'): Promise<{ token?: string; terminal: boolean }> {
    try {
      const credential = await resolveCredential(this.credentials, call, this.refreshDriver, call.context.cancellationSignal);
      const token = (kind === 'bot' ? credential.botToken : credential.userToken)?.reveal();
      return { ...(token && isHeaderSafeCredential(token) ? { token } : {}), terminal: !token || !isHeaderSafeCredential(token) };
    } catch (error) {
      return { terminal: error instanceof ReauthorizationRequiredError || error instanceof TypeError };
    }
  }

  /** Refresh once after Slack rejects an access token; the host owns persistence. */
  async refreshRejectedToken(call: Call, kind: 'bot' | 'user', currentToken: string): Promise<{ token?: string; terminal: boolean }> {
    try {
      const credential = await resolveCredentialAfterRejection(this.credentials, call, this.refreshDriver, call.context.cancellationSignal);
      const token = (kind === 'bot' ? credential.botToken : credential.userToken)?.reveal();
      return { ...(token && token !== currentToken && isHeaderSafeCredential(token) ? { token } : {}), terminal: !token || token === currentToken || !isHeaderSafeCredential(token) };
    } catch (error) { return { terminal: error instanceof ReauthorizationRequiredError }; }
  }

  /** Make one Slack HTTP request with Go-compatible body and status handling. */
  async request(method: 'GET' | 'POST', path: string, token: string, query?: URLSearchParams, payload?: Record<string, string>, signal?: AbortSignal): Promise<SlackExchange> {
    const ordered = query ? new URLSearchParams([...query].sort(([a], [b]) => a.localeCompare(b, 'en'))) : undefined;
    const url = `${this.endpoint}/${path}${ordered && [...ordered].length ? `?${ordered.toString()}` : ''}`;
    const headers: Record<string, string> = { Authorization: `Bearer ${token}` };
    if (method === 'POST') headers['Content-Type'] = 'application/json; charset=utf-8';
    const request: ProviderRequest = { url, method, headers, maxResponseBytes: this.maxResponseBytes, addressPolicy: this.endpoint.startsWith('https://slack.com/') ? 'publicHTTPS' : 'privateNetworkHTTP', timeoutMs: 25_000, ...(signal ? { signal } : {}), ...(payload ? { body: Buffer.from(JSON.stringify(payload)) } : {}) };
    let received: ProviderResponse;
    try { received = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError) return { status: error.status ?? 0, headers: error.headers ?? {}, body: undefined, error: 'tooLarge' };
      return { status: 0, headers: {}, body: undefined, error: error instanceof ProviderTransportError && error.isProvablyNotSent ? 'notSent' : 'transport' };
    }
    if (!received.body.length) return { status: received.status, headers: received.headers, body: undefined, error: 'invalid' };
    try {
      const body: unknown = JSON.parse(Buffer.from(received.body).toString('utf8'));
      if (!isSlackResponse(body)) return { status: received.status, headers: received.headers, body: undefined, error: 'invalid' };
      return { status: received.status, headers: received.headers, body };
    } catch { return { status: received.status, headers: received.headers, body: undefined, error: 'invalid' }; }
  }

  /** Retry one authenticated read after host-coordinated renewal. */
  async get(call: Call, token: string, path: string, query: URLSearchParams): Promise<SlackExchange> {
    const first = await this.request('GET', path, token, query, undefined, call.context.cancellationSignal);
    if (first.error || !isCredentialRejected(first) || !('resolveAfterRejection' in this.credentials)) return first;
    const replacement = await this.refreshRejectedToken(call, 'user', token);
    return replacement.token ? this.request('GET', path, replacement.token, query, undefined, call.context.cancellationSignal) : first;
  }

  /** A receipt contains only provider IDs and safe timing. */
  receipt(call: Call, response?: SlackExchange, objectId = ''): ReceiptDraft {
    return { callId: call.id, provider: 'slack', observedAt: this.now().toISOString(), ...(call.idempotencyKey ? { idempotencyKey: call.idempotencyKey } : {}), ...(objectId ? { providerObjectId: objectId } : {}), ...(response ? { providerRequestId: header(response, 'x-slack-req-id') } : {}) };
  }
}

/** Read a case-insensitive HTTP header. */
export function header(response: SlackExchange, name: string): string {
  const value = Object.entries(response.headers).find(([key]) => key.toLowerCase() === name.toLowerCase())?.[1];
  return Array.isArray(value) ? value[0] ?? '' : value ?? '';
}

/** Slack's 401 and documented body codes reject credentials. */
export function isCredentialRejected(response: SlackExchange): boolean {
  const body = response.body as Record<string, unknown> | undefined;
  return response.status === 401 || ['invalid_auth', 'token_expired', 'token_revoked'].includes(String(body?.error ?? ''));
}

/** Construct the provider-safe failure contract. */
export function failure(operation: string, kind: Failure['kind'], message: string): Failure { return { provider: 'slack', operation, kind, message }; }

/** Parse Slack's integer Retry-After seconds without accepting dates. */
export function retryAfter(response: SlackExchange): number {
  const value = header(response, 'retry-after');
  if (!/^[1-9][0-9]*$/.test(value)) return 0;
  const seconds = Number(value);
  return Number.isSafeInteger(seconds) ? seconds * 1000 : 0;
}

/** Reject wrong-typed provider fields like Go's json.Unmarshal. */
function isSlackResponse(value: unknown): value is Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return false;
  const item = value as Record<string, unknown>;
  for (const key of ['ok']) if (item[key] != null && typeof item[key] !== 'boolean') return false;
  for (const key of ['error', 'channel', 'ts']) if (item[key] != null && typeof item[key] !== 'string') return false;
  if (item.messages != null && (!Array.isArray(item.messages) || item.messages.some(message => !isSlackMessage(message)))) return false;
  if (item.message != null && !isSlackMessage(item.message)) return false;
  if (item.response_metadata != null) {
    if (!item.response_metadata || typeof item.response_metadata !== 'object' || Array.isArray(item.response_metadata)) return false;
    const cursor = (item.response_metadata as Record<string, unknown>).next_cursor;
    if (cursor !== undefined && typeof cursor !== 'string') return false;
  }
  return true;
}

/** Check Go-decoded Slack message field types. */
function isSlackMessage(value: unknown): boolean {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return false;
  const item = value as Record<string, unknown>;
  return ['channel', 'ts', 'thread_ts', 'user', 'text'].every(key => item[key] == null || typeof item[key] === 'string');
}
