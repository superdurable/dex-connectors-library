// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, type Call, type CredentialProvider, type Failure, type FailureKind, type ReceiptDraft } from '@superdurable/connectors-sdk';
import { isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import type { Config, Credentials, Response } from './generated/models.js';
import { CreateResponseOperation, RetrieveResponseOperation } from './operations.js';

const defaultEndpoint = 'https://api.openai.com/v1';
const rateLimitHeaders = ['x-ratelimit-limit-requests', 'x-ratelimit-remaining-requests', 'x-ratelimit-reset-requests', 'x-ratelimit-limit-tokens', 'x-ratelimit-remaining-tokens', 'x-ratelimit-reset-tokens'];

/** Injectable HTTP sender for deterministic provider tests. */
export type OpenAIRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;

/** Provider transport and clock overrides for tests. */
export interface OpenAIClientOptions {
  /** Override the SDK's no-redirect HTTP sender. */ send?: OpenAIRequestSender;
  /** Override receipt and retry date time. */ now?: () => Date;
}

/** Authenticated OpenAI Responses client. Credentials are resolved for every request. */
export class OpenAIClient {
  readonly endpoint: string;
  readonly model: string;
  readonly maxResponseBytes: number;
  readonly maxSseEventBytes: number;
  private readonly send: OpenAIRequestSender;
  private readonly now: () => Date;

  /** Validate configuration without making a provider call. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: OpenAIClientOptions = {}) {
    if (!credentials) throw new TypeError('credential provider is required');
    this.model = config.model?.trim() || 'gpt-6-sol';
    if (!isPrintableModelID(this.model)) throw new TypeError('OpenAI model must be 1 to 256 printable ASCII characters without spaces');
    const endpoint = config.endpoint || defaultEndpoint;
    let parsed: URL;
    try { parsed = new URL(endpoint); } catch { throw new TypeError('OpenAI endpoint must be absolute'); }
    if (!parsed.hostname) throw new TypeError('OpenAI endpoint must be absolute');
    const rawAuthority = /^\w+:\/\/([^/?#]+)/.exec(endpoint)?.[1] ?? '';
    const rawHost = rawAuthority.replace(/^.*@/, '').replace(/:\d+$/, '');
    if (parsed.protocol !== 'https:' && rawHost !== '127.0.0.1' && rawHost !== 'localhost') throw new TypeError('OpenAI endpoint must use HTTPS');
    this.endpoint = endpoint.replace(/\/+$/, '');
    this.maxResponseBytes = config.maxResponseBytes || 8_388_608;
    this.maxSseEventBytes = config.maxSseEventBytes || 1_048_576;
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes < 1 || !Number.isSafeInteger(this.maxSseEventBytes) || this.maxSseEventBytes < 1) throw new TypeError('OpenAI response limits must be positive');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the stored-response creation Mutation. */
  createResponse(): CreateResponseOperation { return new CreateResponseOperation(this); }
  /** Bind the stored-response retrieval Query. */
  retrieveResponse(): RetrieveResponseOperation { return new RetrieveResponseOperation(this); }

  /** Resolve credentials and encode one request before dispatch. */
  async prepareRequest(call: Call, method: string, path: string, payload?: unknown, onResponseChunk?: ProviderRequest['onResponseChunk'], timeoutMs = 120_000): Promise<ProviderRequest> {
    let resolved: Credentials;
    try { resolved = await this.credentials.resolve(call, call.context.cancellationSignal); }
    catch { throw new CredentialFailure('connection credentials are unavailable'); }
    if (!(resolved.apiKey instanceof SecretString) || !isHeaderSafeCredential(resolved.apiKey.reveal())) throw new CredentialFailure('connection credentials are invalid');
    let body: Uint8Array | undefined;
    if (payload !== undefined) {
      try {
        if (!isJSONSerializable(payload)) throw new TypeError('request is not JSON serializable');
        const encoded = JSON.stringify(payload);
        if (encoded === undefined) throw new TypeError('request is not JSON serializable');
        body = Buffer.from(encoded);
      } catch { throw new EncodingFailure(); }
    }
    const request: ProviderRequest = {
      url: this.endpoint + path, method,
      headers: { Authorization: `Bearer ${resolved.apiKey.reveal()}`, 'Content-Type': 'application/json', ...(method === 'POST' ? { 'X-Client-Request-Id': call.id } : {}) },
      ...(body ? { body } : {}), maxResponseBytes: this.maxResponseBytes,
      maxErrorResponseBytes: 0, ignoreErrorBodyReadFailure: true,
      addressPolicy: 'unrestricted', timeoutMs, signal: call.context.cancellationSignal,
      ...(onResponseChunk ? { onResponseChunk } : {}),
    };
    return request;
  }

  /** Send one already prepared provider request. */
  async sendPreparedRequest(request: ProviderRequest): Promise<ProviderResponse> { return this.send(request); }

  /** Resolve credentials and issue one bounded provider request. */
  async request(call: Call, method: string, path: string, payload?: unknown, onResponseChunk?: ProviderRequest['onResponseChunk'], timeoutMs = 120_000): Promise<ProviderResponse> {
    return this.sendPreparedRequest(await this.prepareRequest(call, method, path, payload, onResponseChunk, timeoutMs));
  }

  /** Correlate an exchange without persisting credentials or response bodies. */
  receipt(call: Call, response: ProviderResponse | undefined, responseId = ''): ReceiptDraft {
    const metadata: Record<string, string> = {};
    for (const name of rateLimitHeaders) {
      const value = header(response, name);
      if (value) metadata[name] = value;
    }
    return { callId: call.id, provider: 'openai', observedAt: this.now().toISOString(),
      ...(call.idempotencyKey ? { idempotencyKey: call.idempotencyKey } : {}),
      ...(responseId ? { providerObjectId: responseId } : {}),
      ...(header(response, 'x-request-id') ? { providerRequestId: header(response, 'x-request-id') } : {}),
      ...(Object.keys(metadata).length ? { metadata } : {}),
    };
  }

  /** Decode the exact Go wire fields and concatenate output text. */
  decodeResponse(body: Uint8Array): Response {
    let document: unknown;
    try { document = JSON.parse(Buffer.from(body).toString('utf8')) as unknown; }
    catch { throw new InvalidResponseError(); }
    if (!isObject(document)) throw new InvalidResponseError();
    const id = stringField(document, 'id');
    const model = stringField(document, 'model');
    const status = stringField(document, 'status');
    const output = arrayField(document, 'output');
    let outputText = '';
    for (const item of output) {
      if (!isObject(item)) throw new InvalidResponseError();
      for (const content of arrayField(item, 'content')) {
        if (!isObject(content)) throw new InvalidResponseError();
        if (stringField(content, 'type') === 'output_text') outputText += stringField(content, 'text');
      }
    }
    const usage = objectField(document, 'usage');
    const inputDetails = objectField(usage, 'input_tokens_details');
    const outputDetails = objectField(usage, 'output_tokens_details');
    return { id, model, status, outputText, usage: {
      inputTokens: integerField(usage, 'input_tokens'), cachedInputTokens: integerField(inputDetails, 'cached_tokens'),
      outputTokens: integerField(usage, 'output_tokens'), reasoningOutputTokens: integerField(outputDetails, 'reasoning_tokens'),
      totalTokens: integerField(usage, 'total_tokens'),
    } };
  }

  /** Parse OpenAI Retry-After under the SDK's one-hour cap. */
  retryAfter(response: ProviderResponse): number { return parseRetryAfter(header(response, 'retry-after'), this.now()); }
}

/** Provider credential lookup failed before transmission. */
export class CredentialFailure extends Error { constructor(message: string) { super(message); } }
/** Request JSON encoding failed before transmission. */
export class EncodingFailure extends Error { constructor() { super('request is not JSON serializable'); } }
/** Provider response did not match the Go decoder's wire shape. */
export class InvalidResponseError extends Error { constructor() { super('provider response is invalid'); } }

/** Construct a secret-safe connector failure. */
export function openAIFailure(kind: FailureKind, operation: string, message: string): Failure { return { kind, provider: 'openai', operation, message }; }
/** Validate a provider response ID before read-back. */
export function isSafeResponseID(id: string): boolean { return /^[A-Za-z0-9_-]{1,128}$/.test(id); }
/** Validate OpenAI model identifiers as the Go connector does. */
export function isPrintableModelID(model: string): boolean { return model.length >= 1 && model.length <= 256 && /^[!-~]+$/.test(model); }
/** Escape one path segment like Go's url.PathEscape. */
export function pathEscape(value: string): string { return encodeURIComponent(value).replace(/%24/g, '$').replace(/%26/g, '&').replace(/%2B/g, '+').replace(/%3D/g, '=').replace(/%3A/g, ':').replace(/%40/g, '@').replace(/[!'()*]/g, character => `%${character.charCodeAt(0).toString(16).toUpperCase()}`); }
/** Return one case-insensitive HTTP header as a scalar. */
export function header(response: ProviderResponse | undefined, name: string): string {
  const value = response?.headers[name];
  return typeof value === 'string' ? value : Array.isArray(value) ? value[0] ?? '' : '';
}
function isObject(value: unknown): value is Record<string, unknown> { return value !== null && typeof value === 'object' && !Array.isArray(value); }
function stringField(object: Record<string, unknown>, name: string): string { const value = object[name]; if (value === undefined || value === null) return ''; if (typeof value !== 'string') throw new InvalidResponseError(); return value; }
function integerField(object: Record<string, unknown>, name: string): number { const value = object[name]; if (value === undefined || value === null) return 0; if (typeof value !== 'number' || !Number.isSafeInteger(value)) throw new InvalidResponseError(); return value; }
function objectField(object: Record<string, unknown>, name: string): Record<string, unknown> { const value = object[name]; if (value === undefined || value === null) return {}; if (!isObject(value)) throw new InvalidResponseError(); return value; }
function arrayField(object: Record<string, unknown>, name: string): unknown[] { const value = object[name]; if (value === undefined || value === null) return []; if (!Array.isArray(value)) throw new InvalidResponseError(); return value; }

function isJSONSerializable(value: unknown, seen = new Set<object>()): boolean {
  if (typeof value === 'function' || typeof value === 'symbol' || typeof value === 'bigint' || typeof value === 'undefined') return false;
  if (typeof value === 'number' && !Number.isFinite(value)) return false;
  if (value === null || typeof value !== 'object') return true;
  if (seen.has(value)) return false;
  seen.add(value);
  const valid = Object.values(value).every(child => isJSONSerializable(child, seen));
  seen.delete(value);
  return valid;
}
