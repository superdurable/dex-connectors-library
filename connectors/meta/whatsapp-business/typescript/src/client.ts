// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newMutationBranch, newMutationRetry, newMutationUncertain, newQueryBranch, newQueryRetry, type Call, type CallID, type CredentialProvider, type Failure, type Mutation, type MutationAttempt, type MutationDefinition, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, ProviderTransportError, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { CheckpointError, InsufficientTimeError, newFence } from '@superdurable/connectors-sdk/singledispatch';
import { ListPhoneNumbersDefinition, SendTemplateMessageDefinition, SendTextMessageDefinition } from './generated/definitions.js';
import type { BusinessPhoneNumber, Config, Credentials, ListPhoneNumbersInput, PhoneNumbersPage, SendTemplateMessageInput, SendTextMessageInput, SentMessage } from './generated/models.js';

const numericID = /^[0-9]{1,40}$/;
const recipientID = /^[1-9][0-9]{5,14}$/;
const templateName = /^[a-z0-9_]{1,512}$/;
const languageCode = /^[a-z]{2,3}(?:_[A-Z]{2})?$/;
const emptyPage = (): PhoneNumbersPage => ({ numbers: [], nextCursor: '' });
const emptyMessage = (): SentMessage => ({ messageId: '' });

/** Optional provider transport for tests and local examples. */
export interface WhatsAppBusinessClientOptions {
  /** One bounded provider request sender. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

interface ProviderOutcome {
  body?: Uint8Array;
  branch?: string;
  failure?: Failure;
  retry?: boolean;
  retryAfterMs?: number;
}

/** Authenticated Meta client with call-time system-user token resolution. */
export class WhatsAppBusinessClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly sendRequest: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate configuration without contacting Meta. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: WhatsAppBusinessClientOptions = {}) {
    if (!credentials) throw new TypeError('WhatsApp Business credential source is required');
    this.baseUrl = validateBaseURL(config.baseUrl || 'https://graph.facebook.com/v25.0');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0) throw new TypeError('Meta timeout is invalid');
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('Meta response limit is invalid');
    this.sendRequest = options.send ?? sendProviderRequest;
  }

  /** Bind the WABA phone-number Query. */ listPhoneNumbers(): ListPhoneNumbersOperation { return new ListPhoneNumbersOperation(this); }
  /** Bind the plain-text Mutation. */ sendTextMessage(): SendTextMessageOperation {
    return new SendTextMessageOperation((call, input) => this.sendTextMessageAttempt(call, input));
  }
  /** Bind the approved-template Mutation. */ sendTemplateMessage(): SendTemplateMessageOperation {
    return new SendTemplateMessageOperation((call, input) => this.sendTemplateMessageAttempt(call, input));
  }

  private async sendTextMessageAttempt(call: Call, input: SendTextMessageInput): Promise<MutationAttempt<SentMessage>> {
    const operation = 'sendTextMessage';
    if (!validAddress(input) || typeof input.text !== 'string' || !input.text || !validUTF8(input.text) || Buffer.byteLength(input.text, 'utf8') > 4096) {
      return newMutationBranch('defect', emptyMessage(), {}, failure(operation, 'VALIDATION', 'sender, recipient, or text is invalid'));
    }
    const payload = { messaging_product: 'whatsapp', recipient_type: 'individual', to: input.recipient, type: 'text', text: { preview_url: false, body: input.text } };
    return this.sendMessage(call, operation, input.phoneNumberId, payload, SendTextMessageDefinition);
  }

  private async sendTemplateMessageAttempt(call: Call, input: SendTemplateMessageInput): Promise<MutationAttempt<SentMessage>> {
    const operation = 'sendTemplateMessage';
    if (!validAddress(input) || typeof input.templateName !== 'string' || !templateName.test(input.templateName) || typeof input.languageCode !== 'string' || !languageCode.test(input.languageCode) || (input.bodyParameters !== undefined && (!Array.isArray(input.bodyParameters) || input.bodyParameters.length > 50))) {
      return newMutationBranch('defect', emptyMessage(), {}, failure(operation, 'VALIDATION', 'sender, recipient, or template is invalid'));
    }
    const parameters: Array<{ type: string; text: string }> = [];
    for (const value of input.bodyParameters ?? []) {
      if (typeof value !== 'string' || !value || !validUTF8(value) || Buffer.byteLength(value, 'utf8') > 1024) {
        return newMutationBranch('defect', emptyMessage(), {}, failure(operation, 'VALIDATION', 'template body parameter is invalid'));
      }
      parameters.push({ type: 'text', text: value });
    }
    const template: { name: string; language: { code: string }; components?: Array<{ type: string; parameters: typeof parameters }> } = {
      name: input.templateName, language: { code: input.languageCode },
    };
    if (parameters.length > 0) template.components = [{ type: 'body', parameters }];
    const payload = { messaging_product: 'whatsapp', recipient_type: 'individual', to: input.recipient, type: 'template', template };
    return this.sendMessage(call, operation, input.phoneNumberId, payload, SendTemplateMessageDefinition);
  }

  private async sendMessage(call: Call, operation: string, phoneNumberID: string, payload: object, definition: MutationDefinition): Promise<MutationAttempt<SentMessage>> {
    const zero = emptyMessage();
    let fence: ReturnType<typeof newFence>;
    try { fence = newFence(call, definition.stepDefaults.executeMethodTimeoutMs ?? 30_000); }
    catch { return newMutationBranch('defect', zero, {}, failure(operation, 'LOCAL_DEFECT', 'Meta dispatch fence is unavailable')); }
    if (fence.decide().kind !== 'sendAllowed') return newMutationUncertain(zero, failure(operation, 'TRANSPORT', 'Meta send outcome cannot be confirmed; do not resend'));
    const token = await this.resolveToken(call);
    if (!token) return newMutationBranch('defect', zero, {}, failure(operation, 'AUTHENTICATION', 'connection credentials are unavailable or invalid'));
    let body: Buffer;
    try { body = Buffer.from(JSON.stringify(payload), 'utf8'); }
    catch { return newMutationBranch('defect', zero, {}, failure(operation, 'VALIDATION', 'message request is invalid or too large')); }
    if (body.length > 65_536) return newMutationBranch('defect', zero, {}, failure(operation, 'VALIDATION', 'message request is invalid or too large'));
    const request: ProviderRequest = {
      url: `${this.baseUrl}/${phoneNumberID}/messages`, method: 'POST',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
      body, maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
      addressPolicy: this.baseUrl.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP',
      signal: call.context.cancellationSignal,
    };
    let response: ProviderResponse;
    try { response = await fence.dispatch(signal => this.sendRequest({ ...request, signal })); }
    catch (error) {
      if (error instanceof CheckpointError || error instanceof InsufficientTimeError) return newMutationRetry(failure(operation, 'TRANSPORT', 'Meta dispatch was not started'));
      if (error instanceof ProviderTransportError && error.isProvablyNotSent) {
        try { await fence.recordCanResend(); return newMutationRetry(failure(operation, 'TRANSPORT', 'Meta could not be reached before sending')); }
        catch { return newMutationUncertain(zero, failure(operation, 'TRANSPORT', 'Meta message outcome is unknown')); }
      }
      if (error instanceof BodyTooLargeError && error.status !== undefined) {
        if (error.status === 200) return newMutationUncertain(zero, failure(operation, 'TRANSPORT', 'Meta send response could not be confirmed'));
        response = { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() };
      } else return newMutationUncertain(zero, failure(operation, 'TRANSPORT', 'Meta message outcome is unknown'));
    }
    // A Graph or Cloud API throttling code means Meta refused the send before processing it.
    const isRateLimited = response.status === 429 || (response.status >= 400 && response.status < 500 && decodeGraphError(response.body)?.isThrottled === true);
    if (isRateLimited || response.status === 503) {
      try { await fence.recordCanResend(); }
      catch { return newMutationRetry(failure(operation, 'AVAILABILITY', 'Meta retry checkpoint is unavailable')); }
      const kind = isRateLimited ? 'RATE_LIMIT' : 'AVAILABILITY';
      return newMutationRetry(failure(operation, kind, 'Meta did not process the send'), parseRetryAfter(headerValue(response.headers['retry-after'])));
    }
    if (response.status >= 500 || response.status === 408 || response.status === 409) return newMutationUncertain(zero, failure(operation, 'AVAILABILITY', 'Meta message outcome is unknown'));
    if (response.status >= 400 && response.status < 500) {
      const kind = response.status === 401 ? 'AUTHENTICATION' : response.status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION';
      return newMutationBranch('providerRejected', zero, {}, failure(operation, kind, `Meta rejected HTTP ${response.status}`));
    }
    const document = parseJSON(response.body);
    const messages = isObject(document) ? document.messages : undefined;
    const message = Array.isArray(messages) && messages.length === 1 ? messages[0] : undefined;
    const messageID = isObject(message) ? message.id : undefined;
    if (response.status !== 200 || !isObject(document) || document.messaging_product !== 'whatsapp' || typeof messageID !== 'string' || !messageID.startsWith('wamid.')) {
      return newMutationUncertain(zero, failure(operation, 'PROTOCOL', 'Meta send response is incomplete'));
    }
    try { await fence.recordProviderId(messageID); }
    catch { return newMutationUncertain(zero, failure(operation, 'LOCAL_DEFECT', 'Meta message ID could not be checkpointed')); }
    return newMutationBranch('sent', { messageId: messageID });
  }

  private async resolveToken(call: Call): Promise<string | undefined> {
    try {
      const resolved = await this.credentials.resolve(call, call.context.cancellationSignal);
      const token = resolved.accessToken instanceof SecretString ? resolved.accessToken.reveal() : '';
      return token && isHeaderSafeCredential(token) ? token : undefined;
    } catch { return undefined; }
  }

  /** Read one page or return a provider decision without exposing its body. */
  async exchange(call: Call, operation: string, path: string): Promise<ProviderOutcome> {
    const token = await this.resolveToken(call);
    if (!token) return { branch: 'defect', failure: failure(operation, 'AUTHENTICATION', 'connection credentials are unavailable or invalid') };
    const request: ProviderRequest = {
      url: `${this.baseUrl}${path}`, method: 'GET',
      headers: { Accept: 'application/json', Authorization: `Bearer ${token}`, 'User-Agent': 'Dex Connector (https://superdurable.io)' },
      maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
      addressPolicy: this.baseUrl.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP',
      signal: call.context.cancellationSignal,
    };
    let response: ProviderResponse;
    try { response = await this.sendRequest(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) {
        response = { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() };
        if (response.status === 200) return { branch: 'invalidResponse', failure: failure(operation, 'RESPONSE_TOO_LARGE', 'Meta response exceeded the configured limit') };
      } else return { retry: true, failure: failure(operation, 'TRANSPORT', 'Meta request failed') };
    }
    const status = response.status;
    const retryAfter = parseRetryAfter(headerValue(response.headers['retry-after']));
    if (Buffer.from(response.body).includes(token)) return { branch: 'defect', failure: failure(operation, 'AUTHENTICATION', 'connection credentials are unavailable or invalid') };
    if (status === 429) return { retry: true, retryAfterMs: retryAfter, failure: failure(operation, 'RATE_LIMIT', 'Meta rate limited the request') };
    if (status === 408 || status >= 500) return { retry: true, retryAfterMs: retryAfter, failure: failure(operation, 'AVAILABILITY', 'Meta is temporarily unavailable') };
    const graphError = status >= 400 ? decodeGraphError(response.body) : undefined;
    if (graphError?.isThrottled) return { retry: true, retryAfterMs: retryAfter, failure: failure(operation, 'RATE_LIMIT', 'Meta rate limited the request') };
    if (graphError?.isTransient) return { retry: true, retryAfterMs: retryAfter, failure: failure(operation, 'AVAILABILITY', 'Meta reported a transient error') };
    if (status === 404) return { branch: 'notFound', failure: failure(operation, 'NOT_FOUND', 'Meta did not find the WhatsApp Business Account') };
    if (status !== 200) return { branch: 'providerRejected', failure: failure(operation, 'PROVIDER_REJECTION', `Meta rejected HTTP ${status}`) };
    if (response.body.length > this.maxResponseBytes) return { branch: 'invalidResponse', failure: failure(operation, 'RESPONSE_TOO_LARGE', 'Meta response exceeded the configured limit') };
    return { body: response.body };
  }
}

/** Read one bounded WABA phone-number page. */
export class ListPhoneNumbersOperation implements Query<ListPhoneNumbersInput, PhoneNumbersPage> {
  /** Bind the provider client. */ constructor(private readonly client: WhatsAppBusinessClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return ListPhoneNumbersDefinition; }
  /** Project phone identities or classify a provider failure. */
  async invoke(call: Call, input: ListPhoneNumbersInput): Promise<QueryAttempt<PhoneNumbersPage>> {
    const operation = 'listPhoneNumbers';
    if (!input || typeof input.businessAccountId !== 'string' || !numericID.test(input.businessAccountId) || (input.after !== undefined && (typeof input.after !== 'string' || input.after.length > 4096 || /[\r\n\0]/.test(input.after)))) {
      return newQueryBranch('defect', emptyPage(), {}, failure(operation, 'VALIDATION', 'WABA ID or page cursor is invalid'));
    }
    const query = input.after ? `?${new URLSearchParams({ after: input.after })}` : '';
    const result = await this.client.exchange(call, operation, `/${input.businessAccountId}/phone_numbers${query}`);
    if (result.retry) return newQueryRetry(result.failure!, result.retryAfterMs);
    if (result.failure) return newQueryBranch(result.branch!, emptyPage(), {}, result.failure);
    const document = parseJSON(result.body!);
    if (!isObject(document) || !Array.isArray(document.data) || document.data.length > 100) return invalidPage();
    const paging = document.paging ?? {};
    if (!isObject(paging)) return invalidPage();
    const cursors = paging.cursors ?? {};
    if (!isObject(cursors)) return invalidPage();
    const after = cursors.after ?? '';
    if (typeof after !== 'string' || after.length > 4096) return invalidPage();
    const numbers: BusinessPhoneNumber[] = [];
    for (const item of document.data) {
      if (!isObject(item) || typeof item.id !== 'string' || !numericID.test(item.id) || typeof item.verified_name !== 'string' || !item.verified_name || typeof item.display_phone_number !== 'string' || !item.display_phone_number) return invalidPage();
      numbers.push({ id: item.id, verifiedName: item.verified_name, displayPhoneNumber: item.display_phone_number });
    }
    return newQueryBranch('listed', { numbers, nextCursor: after });
  }
}

/** Send one plain-text WhatsApp message through a durable dispatch fence. */
export class SendTextMessageOperation implements Mutation<SendTextMessageInput, SentMessage> {
  /** Bind a client's text send callback. */ constructor(private readonly send: (call: Call, input: SendTextMessageInput) => Promise<MutationAttempt<SentMessage>>) {}
  /** Return the generated Mutation contract. */ definition(): MutationDefinition { return SendTextMessageDefinition; }
  /** Record the Call ID; Meta does not deduplicate with it. */ idempotencyKey(callID: CallID): string { return callID; }
  /** Send once and report ambiguous outcomes as uncertain. */ invoke(call: Call, input: SendTextMessageInput): Promise<MutationAttempt<SentMessage>> { return this.send(call, input); }
}

/** Send one approved WhatsApp template through a durable dispatch fence. */
export class SendTemplateMessageOperation implements Mutation<SendTemplateMessageInput, SentMessage> {
  /** Bind a client's template send callback. */ constructor(private readonly send: (call: Call, input: SendTemplateMessageInput) => Promise<MutationAttempt<SentMessage>>) {}
  /** Return the generated Mutation contract. */ definition(): MutationDefinition { return SendTemplateMessageDefinition; }
  /** Record the Call ID; Meta does not deduplicate with it. */ idempotencyKey(callID: CallID): string { return callID; }
  /** Send once and report ambiguous outcomes as uncertain. */ invoke(call: Call, input: SendTemplateMessageInput): Promise<MutationAttempt<SentMessage>> { return this.send(call, input); }
}

function validAddress(input: { phoneNumberId: unknown; recipient: unknown } | null | undefined): input is { phoneNumberId: string; recipient: string } {
  return !!input && typeof input.phoneNumberId === 'string' && numericID.test(input.phoneNumberId) && typeof input.recipient === 'string' && recipientID.test(input.recipient);
}
function validUTF8(value: string): boolean { return Buffer.from(value, 'utf8').toString('utf8') === value; }
function failure(operation: string, kind: Failure['kind'], message: string): Failure { return { provider: 'whatsapp-business', operation, kind, message }; }
function invalidPage(): QueryAttempt<PhoneNumbersPage> { return newQueryBranch('invalidResponse', emptyPage(), {}, failure('listPhoneNumbers', 'PROTOCOL', 'Meta returned invalid phone-number JSON')); }
/** Decode Meta's Graph API error envelope; undefined means the body is not one. */
function decodeGraphError(body: Uint8Array): { isThrottled: boolean; isTransient: boolean } | undefined {
  const document = parseJSON(body);
  if (!isObject(document) || !isObject(document.error)) return undefined;
  const code = document.error.code ?? 0;
  const isTransient = document.error.is_transient ?? false;
  if (!Number.isSafeInteger(code) || typeof isTransient !== 'boolean') return undefined;
  const numericCode = code as number;
  // Meta documents these Graph and Cloud API rate-limit codes, which it returns with HTTP 4xx.
  const isThrottled = [4, 17, 32, 341, 613, 130429, 131056].includes(numericCode) || (numericCode >= 80_000 && numericCode <= 80_014);
  return { isThrottled, isTransient };
}
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function parseJSON(body: Uint8Array): unknown { try { return JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { return undefined; } }
function headerValue(value: string | string[] | undefined): string { return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
