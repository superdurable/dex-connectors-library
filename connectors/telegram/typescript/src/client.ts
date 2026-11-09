// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  newMutationBranch, newMutationRetry, newMutationUncertain, newQueryBranch, newQueryRetry,
  type Call, type CredentialProvider, type Failure, type Mutation, type MutationAttempt,
  type Query, type QueryAttempt,
} from '@superdurable/connectors-sdk';
import {
  BodyTooLargeError, ProviderTransportError, parseRetryAfter, sendProviderRequest, validateBaseURL,
  type ProviderRequest, type ProviderResponse,
} from '@superdurable/connectors-sdk/providerhttp';
import { CheckpointError, InsufficientTimeError, newFence } from '@superdurable/connectors-sdk/singledispatch';
import { GetChatDefinition, SendMessageDefinition } from './generated/definitions.js';
import type { Chat, Config, Credentials, GetChatInput, SendMessageInput, SentMessage } from './generated/models.js';

const tokenPattern = /^[0-9]+:[A-Za-z0-9_-]+$/;
const usernamePattern = /^@[A-Za-z0-9_]{5,32}$/;
const chatTypes = new Set(['private', 'group', 'supergroup', 'channel']);
const emptyMessage: SentMessage = { chatId: 0, messageId: 0, date: 0 };
const emptyChat: Chat = { id: 0, type: '', title: '', username: '' };

/** Injectable Bot API sender and clock for deterministic provider replay. */
export interface TelegramClientOptions {
  /** Provider sender; omitted uses the SDK's redirect-safe transport. */
  send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Clock for Retry-After dates. */
  now?: () => Date;
}

/** One provider exchange, retaining only safe status and transport evidence. */
export interface TelegramExchange {
  /** HTTP status, or zero when no response arrived. */ status: number;
  /** Complete bounded response body. */ body: Uint8Array;
  /** Retry-After delay in milliseconds. */ retryAfterMs: number;
  /** Safe transport classification without URL or token. */ error?: 'request' | 'tooLarge' | 'read';
  /** Whether the request may have reached Telegram. */ requestSent: boolean;
}

/** Telegram Bot API client with call-time credentials and bounded responses. */
export class TelegramClient {
  /** Provider base URL without a trailing slash. */ readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly credentials: CredentialProvider<Credentials>;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;

  /** Validate configuration without contacting Telegram. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: TelegramClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Telegram configuration and credential source are required');
    this.baseUrl = validateBaseURL(config.baseUrl || 'https://api.telegram.org');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 ||
      !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('Telegram timeout and response limit must be positive integers');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the single-dispatch text Mutation. */ sendMessage(): SendMessageOperation {
    return new SendMessageOperation(this);
  }
  /** Bind the chat metadata Query. */ getChat(): GetChatOperation {
    return new GetChatOperation(this);
  }

  /** Resolve and validate the current bot token without returning it in failures. */
  async botToken(call: Call): Promise<string | undefined> {
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      const token = credentials.botToken.reveal();
      return tokenPattern.test(token) ? token : undefined;
    } catch { return undefined; }
  }

  /** Send one bounded Bot API request and retain positive no-send evidence. */
  async exchange(call: Call, method: 'GET' | 'POST', url: string,
    headers: Record<string, string>, body?: Uint8Array, signal?: AbortSignal): Promise<TelegramExchange> {
    const request: ProviderRequest = {
      url, method, headers, maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
      addressPolicy: url.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP',
      signal: signal ?? call.context.cancellationSignal,
      ...(body ? { body } : {}),
    };
    try {
      const response = await this.send(request);
      return { status: response.status, body: response.body,
        retryAfterMs: this.retryAfter(response.headers), requestSent: true };
    } catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined)
        return { status: error.status, body: new Uint8Array(),
          retryAfterMs: this.retryAfter(error.headers ?? {}), error: 'tooLarge', requestSent: true };
      if (error instanceof ProviderTransportError)
        return { status: 0, body: new Uint8Array(),
          retryAfterMs: 0, error: 'request',
          requestSent: !error.isProvablyNotSent };
      return { status: 0, body: new Uint8Array(), retryAfterMs: 0,
        error: 'request', requestSent: true };
    }
  }

  private retryAfter(headers: ProviderResponse['headers']): number {
    const entry = Object.entries(headers).find(([name]) => name.toLowerCase() === 'retry-after')?.[1];
    return parseRetryAfter(Array.isArray(entry) ? entry[0] ?? '' : entry ?? '', this.now());
  }
}

/** Send one plain text message without repeating an ambiguous dispatch. */
export class SendMessageOperation implements Mutation<SendMessageInput, SentMessage> {
  /** Bind the provider client. */ constructor(private readonly client: TelegramClient) {}
  /** Return the generated Mutation contract. */ definition() { return SendMessageDefinition; }
  /** Keep stable Dex call identity; Telegram receives no deduplication key. */
  idempotencyKey(callId: string): string { return callId; }

  /** Claim a checkpoint before dispatch and classify one Telegram response. */
  async invoke(call: Call, input: SendMessageInput): Promise<MutationAttempt<SentMessage>> {
    let fence: ReturnType<typeof newFence>;
    try { fence = newFence(call, SendMessageDefinition.stepDefaults.executeMethodTimeoutMs ?? 30_000); }
    catch { return sendFailure('defect', 'LOCAL_DEFECT', 'Telegram dispatch fence is unavailable'); }
    if (fence.decide().kind !== 'sendAllowed')
      return sendUncertain('TRANSPORT', 'Telegram send outcome cannot be confirmed; do not resend');
    if (!input || !validChatID(input.chatId) || typeof input.text !== 'string' || !input.text ||
      Buffer.from(input.text).toString() !== input.text || [...input.text].length > 4096)
      return sendFailure('defect', 'VALIDATION', 'chatId or text is invalid');
    const token = await this.client.botToken(call);
    if (!token) return sendFailure('defect', 'AUTHENTICATION', 'bot token is unavailable or invalid');
    const body = Buffer.from(new URLSearchParams({ chat_id: input.chatId, text: input.text }).toString());
    const url = `${this.client.baseUrl}/bot${token}/sendMessage`;
    let exchange: TelegramExchange;
    try {
      exchange = await fence.dispatch(signal => this.client.exchange(call, 'POST', url,
        { Accept: 'application/json', 'Content-Type': 'application/x-www-form-urlencoded' }, body, signal));
    } catch (error) {
      if (error instanceof CheckpointError || error instanceof InsufficientTimeError)
        return sendRetry('TRANSPORT', 'Telegram dispatch was not started');
      return sendUncertain('TRANSPORT', 'Telegram message outcome is unknown');
    }
    if (exchange.error === 'request') {
      if (!exchange.requestSent) {
        try {
          await fence.recordCanResend();
          return sendRetry('TRANSPORT', 'Telegram could not be reached before sending');
        } catch { /* The claimed marker still blocks another send. */ }
      }
      return sendUncertain('TRANSPORT', 'Telegram message outcome is unknown');
    }
    if (exchange.status === 429 || exchange.status === 503) {
      try { await fence.recordCanResend(); }
      catch { return sendRetry('AVAILABILITY', 'Telegram retry checkpoint is unavailable'); }
      return sendRetry(exchange.status === 429 ? 'RATE_LIMIT' : 'AVAILABILITY',
        'Telegram did not process the send', exchange.retryAfterMs);
    }
    if (exchange.status >= 500 || exchange.status === 408 || exchange.status === 409)
      return sendUncertain('AVAILABILITY', 'Telegram message outcome is unknown');
    if (exchange.error) return sendUncertain('TRANSPORT', 'Telegram send response could not be confirmed');
    const envelope = parseJSON(exchange.body);
    if (envelope === undefined) return sendUncertain('PROTOCOL', 'Telegram send response is malformed');
    if (exchange.status >= 400 && exchange.status < 500 && isRecord(envelope) && envelope.ok !== true)
      return sendFailure('providerRejected', rejectionKind(exchange.status),
        `Telegram rejected the message with HTTP ${exchange.status}`);
    const result = isRecord(envelope) ? envelope.result : undefined;
    const chat = isRecord(result) ? result.chat : undefined;
    if (exchange.status !== 200 || !isRecord(envelope) || envelope.ok !== true ||
      !isRecord(result) || !Number.isSafeInteger(result.message_id) || (result.message_id as number) <= 0 ||
      !Number.isSafeInteger(result.date) || (result.date as number) <= 0 ||
      !isRecord(chat) || !Number.isSafeInteger(chat.id) || chat.id === 0)
      return sendUncertain('PROTOCOL', 'Telegram send response is incomplete');
    const messageId = result.message_id as number;
    try { await fence.recordProviderId(String(messageId)); }
    catch { /* The claimed marker still blocks another send. */ }
    return newMutationBranch('sent', { chatId: chat.id as number, messageId, date: result.date as number });
  }
}

/** Read basic metadata for one bot-accessible Telegram chat. */
export class GetChatOperation implements Query<GetChatInput, Chat> {
  /** Bind the provider client. */ constructor(private readonly client: TelegramClient) {}
  /** Return the generated Query contract. */ definition() { return GetChatDefinition; }

  /** Validate the selector and classify one Bot API chat response. */
  async invoke(call: Call, input: GetChatInput): Promise<QueryAttempt<Chat>> {
    if (!input || !validChatID(input.chatId))
      return chatFailure('defect', 'VALIDATION', 'chatId must be a decimal ID or public @username');
    const token = await this.client.botToken(call);
    if (!token) return chatFailure('defect', 'AUTHENTICATION', 'bot token is unavailable or invalid');
    const url = `${this.client.baseUrl}/bot${token}/getChat?${new URLSearchParams({ chat_id: input.chatId })}`;
    const exchange = await this.client.exchange(call, 'GET', url, { Accept: 'application/json' });
    if (exchange.error === 'request') return chatRetry('AVAILABILITY', 'Telegram request failed');
    if (exchange.status === 429)
      return chatRetry('RATE_LIMIT', 'Telegram rate limited the request', exchange.retryAfterMs);
    if (exchange.status === 408 || exchange.status >= 500)
      return chatRetry('AVAILABILITY', 'Telegram is temporarily unavailable', exchange.retryAfterMs);
    if (exchange.error === 'tooLarge')
      return chatFailure('invalidResponse', 'RESPONSE_TOO_LARGE', 'Telegram response exceeded the configured limit');
    if (exchange.error) return chatRetry('AVAILABILITY', 'Telegram response was interrupted');
    const envelope = parseJSON(exchange.body);
    if (!isRecord(envelope)) return chatFailure('invalidResponse', 'PROTOCOL', 'Telegram returned invalid JSON');
    const isOK = envelope.ok === true;
    const errorCode = Number.isSafeInteger(envelope.error_code) ? envelope.error_code as number : 0;
    if (exchange.status === 200 && !isOK && errorCode === 0)
      return chatFailure('invalidResponse', 'PROTOCOL', 'Telegram returned an incomplete response');
    if (!isOK) {
      const status = errorCode || exchange.status;
      if (status === 429) {
        const parameters = isRecord(envelope.parameters) ? envelope.parameters : {};
        const seconds = Number.isSafeInteger(parameters.retry_after) ? parameters.retry_after as number : 0;
        return chatRetry('RATE_LIMIT', 'Telegram rate limited the request', Math.max(seconds, 0) * 1000);
      }
      if (status >= 500) return chatRetry('AVAILABILITY', 'Telegram is temporarily unavailable');
      if (status === 400 && typeof envelope.description === 'string' &&
        envelope.description.toLowerCase().includes('chat not found'))
        return chatFailure('notFound', 'NOT_FOUND', 'Telegram did not find the chat');
      if (status === 401) return chatFailure('providerRejected', 'AUTHENTICATION', 'Telegram rejected the bot token');
      if (status === 403) return chatFailure('providerRejected', 'AUTHORIZATION', 'Telegram denied access to the chat');
      return chatFailure('providerRejected', 'PROVIDER_REJECTION', `Telegram rejected HTTP ${status}`);
    }
    const result = envelope.result;
    if (exchange.status !== 200 || !isRecord(result))
      return chatFailure('invalidResponse', 'PROTOCOL', 'Telegram returned an inconsistent response');
    if (!Number.isSafeInteger(result.id) || result.id === 0 || typeof result.type !== 'string' ||
      !chatTypes.has(result.type) || (result.title !== undefined && typeof result.title !== 'string') ||
      (result.username !== undefined && typeof result.username !== 'string'))
      return chatFailure('invalidResponse', 'PROTOCOL', 'Telegram returned unusable chat data');
    return newQueryBranch('found', { id: result.id as number, type: result.type,
      title: result.title ?? '' as string, username: result.username ?? '' as string });
  }
}

function validChatID(value: unknown): boolean {
  if (typeof value !== 'string') return false;
  if (usernamePattern.test(value)) return true;
  if (!/^-?[0-9]+$/.test(value)) return false;
  try {
    const number = BigInt(value);
    return number !== 0n && number >= -(1n << 63n) && number < (1n << 63n);
  } catch { return false; }
}
function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}
function parseJSON(body: Uint8Array): unknown {
  try { return JSON.parse(Buffer.from(body).toString('utf8')) as unknown; }
  catch { return undefined; }
}
function rejectionKind(status: number): Failure['kind'] {
  return status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION';
}
function failure(operation: string, kind: Failure['kind'], message: string): Failure {
  return { kind, provider: 'telegram', operation, message };
}
function sendFailure(branch: string, kind: Failure['kind'], message: string): MutationAttempt<SentMessage> {
  return newMutationBranch(branch, emptyMessage, {}, failure('sendMessage', kind, message));
}
function sendRetry(kind: Failure['kind'], message: string, after = 0): MutationAttempt<SentMessage> {
  return newMutationRetry(failure('sendMessage', kind, message), after);
}
function sendUncertain(kind: Failure['kind'], message: string): MutationAttempt<SentMessage> {
  return newMutationUncertain(emptyMessage, failure('sendMessage', kind, message));
}
function chatFailure(branch: string, kind: Failure['kind'], message: string): QueryAttempt<Chat> {
  return newQueryBranch(branch, emptyChat, {}, failure('getChat', kind, message));
}
function chatRetry(kind: Failure['kind'], message: string, after = 0): QueryAttempt<Chat> {
  return newQueryRetry(failure('getChat', kind, message), after);
}
