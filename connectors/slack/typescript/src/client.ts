// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { BodyTooLargeError, ProviderTransportError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ManifestCredentialRefreshDriver } from '@superdurable/connectors-sdk/oauthtoken';
import { ReauthorizationRequiredError, defaultTriggerLogger, resolveCredential, resolveCredentialAfterRejection, type Call, type Failure, type ReceiptDraft, type TriggerLogger } from '@superdurable/connectors-sdk';
import { MaxSlackSocketMessageBytes, dialSlackSocket, type SlackSocketDialer } from './socket_mode.js';
import { Authorization, CredentialFields } from './generated/refresh.js';
import type { Config, CredentialSource, Credentials } from './generated/models.js';
import { GetThreadReplyOperation, GetWorkspaceIdentityOperation, ListChannelMessagesOperation, ListChannelsOperation, ListThreadMessagesOperation, PostChannelMessageOperation, PostThreadReplyOperation } from './operations.js';

/** One Slack request exchange, including a status when body decoding failed. */
export interface SlackExchange {
  status: number;
  headers: ProviderResponse['headers'];
  body: unknown;
  error?: 'transport' | 'notSent' | 'tooLarge' | 'invalid';
}

/** Socket Mode and relay timings in milliseconds; tests shorten them. */
export interface SlackTriggerTimings {
  /** Wait after the first failed connection and after Slack asks for a reconnect; doubles per consecutive failure. */ reconnectDelayMs: number;
  /** Cap of the doubled reconnect wait. */ reconnectMaxDelayMs: number;
  /** A connection that receives neither an envelope nor a ping for this long is reconnected. */ readTimeoutMs: number;
}

/** Default Socket Mode timings, matching the Go connector. */
export const DefaultSlackTriggerTimings: Readonly<SlackTriggerTimings> = Object.freeze({ reconnectDelayMs: 1_000, reconnectMaxDelayMs: 30_000, readTimeoutMs: 45_000 });

/** Override the provider sender and receipt clock for local tests, and configure Trigger logging. */
export interface SlackClientOptions {
  /** Provider HTTP sender; omitted uses the SDK's address-checked sender. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Receipt and relayed-request clock. */ now?: () => Date;
  /** Receives Trigger records: Socket Mode connections, ignored messages at DEBUG, and delivery retries; omitted uses defaultTriggerLogger. */ logger?: TriggerLogger;
  /** Opens Socket Mode WebSockets; omitted uses the ws package. */ socketDialer?: SlackSocketDialer;
  /** Socket Mode timings; omitted fields use DefaultSlackTriggerTimings. */ triggerTimings?: Partial<SlackTriggerTimings>;
  /**
   * Acknowledged Socket Mode envelopes that may await delivery on one connection; omitted uses 256, as in Go. A full
   * queue stops reading the socket after the network chunk being parsed, so Slack holds the rest until delivery
   * catches up.
   */
  socketModeDeliveryQueueCapacity?: number;
}

/** Slack Web API client with host-owned renewable bot and user credentials. */
export class SlackClient {
  readonly endpoint: string;
  readonly maxResponseBytes: number;
  readonly maxMessageCharacters: number;
  /** Trigger record destination. */ readonly triggerLogger: TriggerLogger;
  /** Socket Mode WebSocket dialer. */ readonly socketDialer: SlackSocketDialer;
  /** Socket Mode timings. */ readonly triggerTimings: Readonly<SlackTriggerTimings>;
  /** Acknowledged Socket Mode envelopes that may await delivery on one connection. */ readonly socketModeDeliveryQueueCapacity: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Receipt and relayed-request clock. */ readonly now: () => Date;
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
    // The Socket Mode message limit uses maxResponseBytes, and ws would silently drop a limit beyond a signed 32-bit integer.
    if (this.maxResponseBytes > MaxSlackSocketMessageBytes) throw new TypeError('Slack maxResponseBytes must be at most 2147483647');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
    this.triggerLogger = options.logger ?? defaultTriggerLogger;
    this.socketDialer = options.socketDialer ?? ((url, signal) => dialSlackSocket(url, signal, this.maxResponseBytes));
    this.socketModeDeliveryQueueCapacity = options.socketModeDeliveryQueueCapacity ?? 256;
    if (!Number.isSafeInteger(this.socketModeDeliveryQueueCapacity) || this.socketModeDeliveryQueueCapacity < 1) throw new TypeError('Slack Socket Mode delivery queue capacity must be a positive integer');
    this.triggerTimings = Object.freeze({ ...DefaultSlackTriggerTimings, ...options.triggerTimings });
    if (Object.values(this.triggerTimings).some(value => !Number.isSafeInteger(value) || value < 1)) throw new TypeError('Slack Trigger timings must be positive integer milliseconds');
  }

  /** Read the connection's credential for a Trigger source outside a Dex Step, refreshing it when needed. */
  async resolveTriggerCredentials(call: Call, signal: AbortSignal): Promise<Credentials> {
    return resolveCredential(this.credentials, call, this.refreshDriver, signal);
  }

  /**
   * Ask Slack for a Socket Mode WebSocket URL with the app-level token. The rejection names Slack's error
   * code and HTTP status, never the token or URL.
   */
  async openSocketModeConnection(appToken: string, signal: AbortSignal): Promise<string> {
    if (!isHeaderSafeCredential(appToken)) throw new Error('Slack Socket Mode credentials are unavailable: credential app_token is invalid');
    let received: ProviderResponse;
    try {
      received = await this.send({ url: `${this.endpoint}/apps.connections.open`, method: 'POST', headers: { Authorization: `Bearer ${appToken}` }, maxResponseBytes: this.maxResponseBytes,
        addressPolicy: this.endpoint.startsWith('https://slack.com/') ? 'publicHTTPS' : 'privateNetworkHTTP', timeoutMs: 25_000, signal });
    } catch (error) {
      if (error instanceof BodyTooLargeError) throw new Error('read Slack Socket Mode open response');
      throw new Error('open Slack Socket Mode connection: transport failure');
    }
    let decoded: { ok?: unknown; error?: unknown; url?: unknown } = {};
    let isDecoded = true;
    try { decoded = JSON.parse(Buffer.from(received.body).toString('utf8')) as typeof decoded; } catch { isDecoded = false; }
    const isSuccessStatus = received.status >= 200 && received.status < 300;
    if (isSuccessStatus && (!isDecoded || !decoded || typeof decoded !== 'object')) throw new Error('decode Slack Socket Mode open response');
    if (!isSuccessStatus || decoded.ok !== true || typeof decoded.url !== 'string' || !decoded.url) {
      throw new Error(`Slack rejected the Socket Mode connection: ${slackCode(typeof decoded?.error === 'string' ? decoded.error : '')} (HTTP ${received.status})`);
    }
    if (!/^wss:\/\//.test(decoded.url) && !/^ws:\/\/(127\.0\.0\.1|localhost)(:\d+)?\//.test(decoded.url)) throw new Error('Slack returned an invalid Socket Mode URL');
    return decoded.url;
  }

  /** Bind the list channels Query. */ listChannels(): ListChannelsOperation { return new ListChannelsOperation(this); }
  /** Bind the get workspace identity Query. */ getWorkspaceIdentity(): GetWorkspaceIdentityOperation { return new GetWorkspaceIdentityOperation(this); }
  /** Bind the list thread messages Query. */ listThreadMessages(): ListThreadMessagesOperation { return new ListThreadMessagesOperation(this); }
  /** Bind the get thread reply Query. */ getThreadReply(): GetThreadReplyOperation { return new GetThreadReplyOperation(this); }
  /** Bind the list channel messages Query. */ listChannelMessages(): ListChannelMessagesOperation { return new ListChannelMessagesOperation(this); }
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
  async request(method: 'GET' | 'POST', path: string, token: string, query?: URLSearchParams, payload?: Record<string, string>, signal?: AbortSignal, form?: URLSearchParams): Promise<SlackExchange> {
    const queryString = query ? encodeGoQuery(query) : '';
    const url = `${this.endpoint}/${path}${queryString ? `?${queryString}` : ''}`;
    const headers: Record<string, string> = { Authorization: `Bearer ${token}` };
    if (method === 'POST') headers['Content-Type'] = form ? 'application/x-www-form-urlencoded' : 'application/json; charset=utf-8';
    const body = form ? Buffer.from(encodeGoQuery(form)) : payload ? Buffer.from(JSON.stringify(payload)) : undefined;
    const request: ProviderRequest = { url, method, headers, maxResponseBytes: this.maxResponseBytes, addressPolicy: this.endpoint.startsWith('https://slack.com/') ? 'publicHTTPS' : 'privateNetworkHTTP', timeoutMs: 25_000, ...(signal ? { signal } : {}), ...(body ? { body } : {}) };
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

  /** Retry one authenticated read after host-coordinated renewal of the same token kind. */
  async get(call: Call, token: string, path: string, query: URLSearchParams, kind: 'bot' | 'user' = 'user'): Promise<SlackExchange> {
    const first = await this.request('GET', path, token, query, undefined, call.context.cancellationSignal);
    if (first.error || !isCredentialRejected(first) || !('resolveAfterRejection' in this.credentials)) return first;
    const replacement = await this.refreshRejectedToken(call, kind, token);
    return replacement.token ? this.request('GET', path, replacement.token, query, undefined, call.context.cancellationSignal) : first;
  }

  /** Send a form-encoded read, such as auth.test, and refresh a rejected credential once. */
  async postForm(call: Call, token: string, path: string, form: URLSearchParams, kind: 'bot' | 'user'): Promise<SlackExchange> {
    const first = await this.request('POST', path, token, undefined, undefined, call.context.cancellationSignal, form);
    if (first.error || !isCredentialRejected(first) || !('resolveAfterRejection' in this.credentials)) return first;
    const replacement = await this.refreshRejectedToken(call, kind, token);
    return replacement.token ? this.request('POST', path, replacement.token, undefined, undefined, call.context.cancellationSignal, form) : first;
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

/** Return a short snake_case Slack error or reason code, or "unknown", so a record never carries arbitrary response text. */
export function slackCode(code: string): string {
  return code && code.length <= 64 && /^[a-z0-9_]+$/.test(code) ? code : 'unknown';
}

/** Construct the provider-safe failure contract. */
export function failure(operation: string, kind: Failure['kind'], message: string): Failure { return { provider: 'slack', operation, kind, message }; }

/** Parse Slack's integer Retry-After seconds without accepting dates. */
export function retryAfter(response: SlackExchange): number {
  return parseRetryAfter(header(response, 'retry-after'));
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

/** Encode query pairs like Go url.Values.Encode, preserving repeated values. */
export function encodeGoQuery(query: URLSearchParams): string {
  const escape = (value: string) => encodeURIComponent(value).replace(/%20/g, '+').replace(/[!'()*]/g, character => `%${character.charCodeAt(0).toString(16).toUpperCase()}`);
  return [...query].sort(([left], [right]) => Buffer.compare(Buffer.from(left), Buffer.from(right))).map(([key, value]) => `${escape(key)}=${escape(value)}`).join('&');
}
