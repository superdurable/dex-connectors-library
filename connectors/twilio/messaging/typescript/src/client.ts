// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { AddressRefusal, BodyTooLargeError, MaxErrorBodyBytes, ProviderTransportError, isHeaderSafeCredential, parseRetryAfter, readErrorTokens, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { SecretString, resolveCredentialAfterRejection, type Call, type CredentialProvider, type CredentialRefreshDriver, type Failure, type ReceiptDraft } from '@superdurable/connectors-sdk';
import type { Config, Credentials, Message } from './generated/models.js';
import { GetMessageOperation, SendMessageOperation } from './operations.js';

const accountPattern = /^AC[0-9a-f]{32}$/;
const keyPattern = /^SK[0-9a-f]{32}$/;
const messagePattern = /^(SM|MM)[0-9a-f]{32}$/;
const servicePattern = /^MG[0-9a-f]{32}$/;
const phonePattern = /^\+[1-9][0-9]{6,14}$/;
const zeroTime = '0001-01-01T00:00:00Z';
const defaultEndpoint = 'https://api.twilio.com/2010-04-01';
const defaultMaxResponseBytes = 1_048_576;
const requestTimeoutMs = 20_000;
const staticCredentialRefresh: CredentialRefreshDriver<Credentials> = {
  refreshRequired: () => false,
  refresh: async () => { throw new Error('Twilio credentials require manual rotation'); },
};

/** Testable Twilio HTTP sender. */
export type TwilioRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;
/** Constructor dependencies. */
export interface TwilioClientOptions {
  /** Provider transport. */ send?: TwilioRequestSender;
  /** Receipt clock. */ now?: () => Date;
}
/** Complete response or a classified transport failure. */
export type TwilioExchange =
  | { response: ProviderResponse; isTooLarge: boolean; isBodyReadFailed: boolean }
  | { error: Error; isProvablyNotSent: boolean };
/** Validated sender address or Messaging Service. */
export interface MessageSender { fromAddress: string; messagingServiceSid: string }
/** Validated form request. */
export interface SendRequest { recipient: string; sender: MessageSender; form: URLSearchParams }
/** Basic authentication material kept outside Dex state. */
export interface TwilioAuthorization { username: string; password: string }
/** Credential lookup outcome without secret-bearing errors. */
export type AuthorizationResult = { authorization: TwilioAuthorization } | { failure: Failure; isTransient: boolean };

/** Authenticated Twilio Messages API client. */
export class TwilioClient {
  /** Account SID used in the Messages path. */ readonly accountSid: string;
  /** Fully qualified Messages collection URL. */ readonly messagesURL: string;
  /** Optional default sender. */ readonly defaultSender: MessageSender;
  /** Maximum successful response size. */ readonly maxResponseBytes: number;
  private readonly credentials: CredentialProvider<Credentials>;
  private readonly send: TwilioRequestSender;
  private readonly now: () => Date;
  private readonly addressPolicy: ProviderRequest['addressPolicy'];

  /** Validate config and retain provider dependencies. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: TwilioClientOptions = {}) {
    const accountSid = config.accountSid?.trim();
    if (!accountPattern.test(accountSid)) throw new TypeError('Twilio accountSid must be AC followed by 32 lowercase hexadecimal characters');
    this.accountSid = accountSid;
    const endpoint = validateBaseURL(config.endpoint || defaultEndpoint);
    this.messagesURL = `${endpoint}/Accounts/${accountSid}/Messages`;
    this.addressPolicy = 'privateNetworkHTTP';
    this.maxResponseBytes = config.maxResponseBytes || defaultMaxResponseBytes;
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('Twilio maxResponseBytes must be positive');
    this.defaultSender = config.defaultSender?.trim() ? parseSender(config.defaultSender) : { fromAddress: '', messagingServiceSid: '' };
    if (!credentials) throw new TypeError('credential provider is required');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the non-idempotent send mutation. */ sendMessage(): SendMessageOperation { return new SendMessageOperation(this); }
  /** Bind the message status query. */ getMessage(): GetMessageOperation { return new GetMessageOperation(this); }

  /** Resolve current credentials without leaking lookup errors. */
  async authorization(call: Call, operation: string): Promise<AuthorizationResult> {
    let credentials: Credentials;
    try { credentials = await this.credentials.resolve(call, call.context.cancellationSignal); }
    catch (error) {
      if (error instanceof Error && (error.name === 'ReauthorizationRequiredError' || error.message.includes('reauthorization required'))) return { failure: twilioFailure(operation, 'AUTHENTICATION', 'Twilio connection credentials require reauthorization; reconnect the connection'), isTransient: false };
      return { failure: twilioFailure(operation, 'AVAILABILITY', 'Twilio credential lookup is temporarily unavailable'), isTransient: true };
    }
    return this.validateAuthorization(credentials, operation);
  }

  /** Ask the credential host for a newer credential after a rejected send. */
  async authorizationAfterRejection(call: Call): Promise<AuthorizationResult> {
    let credentials: Credentials;
    try { credentials = await resolveCredentialAfterRejection(this.credentials, call, staticCredentialRefresh, call.context.cancellationSignal); }
    catch (error) {
      if (error instanceof TypeError && error.message.includes('does not support refresh')) return { failure: twilioFailure('sendMessage', 'AUTHENTICATION', 'Twilio credential cannot be refreshed after rejection'), isTransient: false };
      if (error instanceof Error && error.name === 'ReauthorizationRequiredError') return { failure: twilioFailure('sendMessage', 'AUTHENTICATION', 'Twilio connection credentials require reauthorization; reconnect the connection'), isTransient: false };
      if (error instanceof Error && error.name === 'RejectedCredentialNotRefreshedError') return { failure: twilioFailure('sendMessage', 'AUTHENTICATION', 'Twilio rejected the message credentials; update the connection'), isTransient: false };
      return { failure: twilioFailure('sendMessage', 'AVAILABILITY', 'Twilio credential refresh is temporarily unavailable'), isTransient: true };
    }
    return this.validateAuthorization(credentials, 'sendMessage');
  }

  private validateAuthorization(credentials: Credentials, operation: string): AuthorizationResult {
    let username = '';
    let password = '';
    if (credentials.authMethodId === 'auth-token' && credentials.authToken instanceof SecretString) {
      username = this.accountSid; password = credentials.authToken.reveal();
    } else if (credentials.authMethodId === 'api-key' && credentials.apiKeySecret instanceof SecretString) {
      username = credentials.apiKeySid?.trim() ?? ''; password = credentials.apiKeySecret.reveal();
      if (!keyPattern.test(username)) return { failure: twilioFailure(operation, 'AUTHENTICATION', 'Twilio api_key_sid must be SK followed by 32 lowercase hexadecimal characters'), isTransient: false };
    } else return { failure: twilioFailure(operation, 'AUTHENTICATION', 'Twilio connection credentials are incomplete; update or reconnect the connection'), isTransient: false };
    if (!isHeaderSafeCredential(password)) return { failure: twilioFailure(operation, 'AUTHENTICATION', 'Twilio credential secret is malformed'), isTransient: false };
    return { authorization: { username, password } };
  }

  /** Send one request; only successful responses use the configured body limit. */
  async exchange(call: Call, authorization: TwilioAuthorization, method: string, url: string, form?: URLSearchParams, signal?: AbortSignal): Promise<TwilioExchange> {
    const body = form ? Buffer.from(form.toString(), 'utf8') : undefined;
    const request: ProviderRequest = {
      url, method, headers: { Authorization: `Basic ${Buffer.from(`${authorization.username}:${authorization.password}`).toString('base64')}`, Accept: 'application/json', ...(body ? { 'Content-Type': 'application/x-www-form-urlencoded' } : {}) },
      ...(body ? { body } : {}), maxResponseBytes: Math.max(this.maxResponseBytes, MaxErrorBodyBytes),
      addressPolicy: this.addressPolicy, timeoutMs: requestTimeoutMs, signal: signal ?? call.context.cancellationSignal,
    };
    try {
      const response = await this.send(request);
      return { response, isTooLarge: response.status >= 200 && response.status < 300 && response.body.length > this.maxResponseBytes, isBodyReadFailed: false };
    } catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined && error.headers) {
        if (error.status < 200 || error.status >= 300) return { response: { status: error.status, headers: error.headers, body: Buffer.alloc(0) }, isTooLarge: false, isBodyReadFailed: false };
        return { response: { status: error.status, headers: error.headers, body: Buffer.alloc(0) }, isTooLarge: true, isBodyReadFailed: false };
      }
      const safe = error instanceof Error ? error : new Error('Twilio transport failed');
      return { error: safe, isProvablyNotSent: error instanceof AddressRefusal || (error instanceof ProviderTransportError && error.isProvablyNotSent) };
    }
  }

  /** Decode a safe Twilio Message subset. */
  decodeMessage(body: Uint8Array, authorization: TwilioAuthorization): Message {
    let resource: Record<string, unknown>;
    try { resource = JSON.parse(Buffer.from(body).toString('utf8')) as Record<string, unknown>; }
    catch { throw new TypeError('Twilio returned an invalid message'); }
    if (!resource || typeof resource !== 'object' || Array.isArray(resource)) throw new TypeError('Twilio returned an invalid message');
    const required = (key: string): string => { const value = resource[key]; if (typeof value !== 'string') throw new TypeError('Twilio returned an invalid message'); return value; };
    const optional = (key: string): string => { const value = resource[key]; if (value == null) return ''; if (typeof value !== 'string') throw new TypeError('Twilio returned an invalid message'); return value; };
    const sid = required('sid'); const accountSid = required('account_sid'); const to = required('to'); const status = required('status');
    if (!messagePattern.test(sid) || accountSid !== this.accountSid || !/^[a-z_]{1,32}$/.test(status) || !safeAddress(to)) throw new TypeError('Twilio returned an invalid message');
    const service = optional('messaging_service_sid'); const from = optional('from'); const direction = optional('direction'); const price = optional('price'); const priceUnit = optional('price_unit');
    if ((service && !servicePattern.test(service)) || (from && !safeAddress(from))) throw new TypeError('Twilio returned an invalid message');
    const message: Message = {
      sid, accountSid, to, status,
      createdAt: twilioTime(optional('date_created')), sentAt: twilioTime(optional('date_sent')), updatedAt: twilioTime(optional('date_updated')),
      ...(service ? { messagingServiceSid: service } : {}), ...(from ? { from } : {}),
      ...(/^[a-z-]{1,32}$/.test(direction) ? { direction } : {}),
      ...(/^-?[0-9]{1,12}(\.[0-9]{1,12})?$/.test(price) ? { price } : {}),
      ...(/^[A-Z]{3}$/.test(priceUnit) ? { priceUnit } : {}),
    };
    for (const [key, target] of [['error_code', 'errorCode'], ['num_segments', 'segmentCount'], ['num_media', 'mediaCount']] as const) {
      const value = resource[key];
      if (value !== null && value !== undefined) {
        if ((typeof value !== 'string' && typeof value !== 'number') || (typeof value === 'number' && !Number.isInteger(value)) || !/^[0-9]{1,9}$/.test(String(value))) throw new TypeError('Twilio returned an invalid message');
        const count = Number(value); if (count !== 0) message[target] = count;
      }
    }
    if ([message.sid, message.accountSid, message.messagingServiceSid, message.from, message.to, message.status, message.direction, message.price, message.priceUnit]
      .some(value => value !== undefined && value.includes(authorization.password))) throw new TypeError('Twilio returned an invalid message');
    return message;
  }

  /** Make a safe receipt with exact provider IDs. */
  receipt(call: Call, response?: ProviderResponse, objectId = '', errorCode = 0): ReceiptDraft {
    const raw = response?.headers['twilio-request-id']; const requestId = Array.isArray(raw) ? raw[0] : raw;
    return { callId: call.id, idempotencyKey: call.idempotencyKey ?? '', provider: 'twilio', observedAt: this.now().toISOString(),
      ...(objectId ? { providerObjectId: objectId } : {}), ...(requestId && safeAddress(requestId) ? { providerRequestId: requestId } : {}),
      ...(errorCode > 0 ? { metadata: { twilioErrorCode: String(errorCode) } } : {}) };
  }

  /** Current receipt time for Retry-After. */ currentTime(): Date { return this.now(); }
}

/** Safe failure without provider content. */
export function twilioFailure(operation: string, kind: Failure['kind'], message: string): Failure { return { kind, provider: 'twilio', operation, message }; }
/** Parse a supported sender. */
export function parseSender(value: string): MessageSender {
  const sender = value.trim();
  if (servicePattern.test(sender)) return { fromAddress: '', messagingServiceSid: sender };
  if (phonePattern.test(sender) || isWhatsApp(sender)) return { fromAddress: sender, messagingServiceSid: '' };
  throw new TypeError('sender must be an E.164 phone number, a whatsapp:+E.164 address, or an MG Messaging Service SID');
}
/** Whether a Twilio address is a WhatsApp E.164 address. */
export function isWhatsApp(value: string): boolean { return value.startsWith('whatsapp:') && phonePattern.test(value.slice(9)); }
/** Build a Go compatible form request. */
export function buildSendRequest(input: { to: string; body?: string; mediaUrls?: string[]; sender?: string }, defaultSender: MessageSender): SendRequest {
  const recipient = input.to.trim(); const isWhatsAppRecipient = isWhatsApp(recipient);
  if (!phonePattern.test(recipient) && !isWhatsAppRecipient) throw new TypeError('recipient must be an E.164 phone number or a whatsapp:+E.164 address');
  const sender = input.sender?.trim() ? parseSender(input.sender) : defaultSender;
  if (!sender.fromAddress && !sender.messagingServiceSid) throw new TypeError('sender is required: set defaultSender on the connection or sender on the input');
  if (sender.fromAddress && isWhatsApp(sender.fromAddress) !== isWhatsAppRecipient) throw new TypeError('sender and recipient must both be whatsapp: addresses or both be phone numbers');
  const body = input.body ?? ''; const media = input.mediaUrls ?? [];
  if ([...body].length > 1600) throw new TypeError('body cannot exceed 1600 characters');
  if (!body.trim() && media.length === 0) throw new TypeError('body or at least one media URL is required');
  if (media.length > 10) throw new TypeError('at most 10 media URLs are allowed');
  const form = new URLSearchParams(); if (body) form.append('Body', body);
  if (sender.fromAddress) form.append('From', sender.fromAddress); else form.append('MessagingServiceSid', sender.messagingServiceSid);
  for (const value of media) { validateMediaURL(value); form.append('MediaUrl', value); }
  form.append('To', recipient); form.sort();
  return { recipient, sender, form };
}
/** Validate a Twilio-hosted media URL. */
export function validateMediaURL(value: string): void {
  if (Buffer.byteLength(value) > 2048 || !/^[!-~]+$/.test(value)) throw new TypeError('media URLs must be printable ASCII without spaces and at most 2048 bytes');
  let parsed: URL; try { parsed = new URL(value); } catch { throw new TypeError('media URLs must be absolute HTTPS URLs without credentials'); }
  if (parsed.protocol !== 'https:' || !parsed.hostname || parsed.username || parsed.password) throw new TypeError('media URLs must be absolute HTTPS URLs without credentials');
}
/** Provider-safe printable text up to 64 bytes. */
export function safeAddress(value: string): boolean { return Buffer.byteLength(value) > 0 && Buffer.byteLength(value) <= 64 && /^[\x20-\x7e]+$/.test(value); }
/** Decode Twilio's RFC1123Z timestamp or return Go's zero instant. */
export function twilioTime(value: string): string {
  const parts = /^(?:Mon|Tue|Wed|Thu|Fri|Sat|Sun), (\d{2}) (Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec) (\d{4}) (\d{2}):(\d{2}):(\d{2}) ([+-])(\d{2})(\d{2})$/.exec(value);
  if (!parts) return zeroTime;
  const [, dayText, monthText, yearText, hourText, minuteText, secondText, sign, offsetHourText, offsetMinuteText] = parts;
  const months = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];
  const year = Number(yearText), month = months.indexOf(monthText ?? ''), day = Number(dayText), hour = Number(hourText), minute = Number(minuteText), second = Number(secondText);
  const offsetHours = Number(offsetHourText), offsetMinutes = Number(offsetMinuteText);
  const monthEnd = new Date(0); monthEnd.setUTCFullYear(year, month + 1, 0);
  const daysInMonth = monthEnd.getUTCDate();
  if (day < 1 || day > daysInMonth || hour > 23 || minute > 59 || second > 59 || offsetMinutes > 59) return zeroTime;
  const instant = new Date(0); instant.setUTCFullYear(year, month, day); instant.setUTCHours(hour, minute, second, 0);
  const offset = (offsetHours * 60 + offsetMinutes) * 60_000;
  const adjusted = new Date(instant.getTime() + (sign === '+' ? -offset : offset));
  return adjusted.toISOString().replace('.000Z', 'Z');
}
/** Read Twilio's numeric error code from an error response. */
export function errorCode(body: Uint8Array): number { const value = readErrorTokens(body, ['/code'])[0]; return value && /^[0-9]{1,9}$/.test(value) ? Number(value) : 0; }
/** Interpret Retry-After. */
export function retryAfter(response: ProviderResponse, now: Date): number { const raw = response.headers['retry-after']; return parseRetryAfter(Array.isArray(raw) ? raw[0] ?? '' : raw ?? '', now); }
/** Shared zero output matching Go's zero Message. */
export const zeroMessage: Message = { accountSid: '', to: '', createdAt: zeroTime, sentAt: zeroTime, updatedAt: zeroTime };
