// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { CreateChatCompletionDefinition } from './generated/definitions.js';
import type { ChatCompletion, Config, CreateChatCompletionInput, Credentials } from './generated/models.js';

const supportedModels = new Set(['lite', 'generalv3', 'pro-128k', '4.0Ultra']);
const emptyCompletion: ChatCompletion = { content: '', model: '', promptTokens: 0, completionTokens: 0, totalTokens: 0 };

/** Send one Spark chat request through a caller-owned transport. */
export type ChatRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;

/** Optional transport dependency for provider tests. */
export interface IFLYTEKClientOptions {
  /** Request sender; production uses the Connector SDK transport. */
  send?: ChatRequestSender;
}

/** Resolve Spark APIPassword for each bounded chat Query. */
export class IFLYTEKClient {
  readonly baseUrl: string;
  readonly timeoutMs: number;
  readonly maxResponseBytes: number;
  readonly model: string;
  readonly maxOutputTokens: number;
  readonly credentials: CredentialProvider<Credentials>;
  readonly send: ChatRequestSender;

  /** Validate settings without making a provider request. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: IFLYTEKClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Spark configuration and credential source are required');
    const baseUrl = config.baseUrl ?? 'https://spark-api-open.xf-yun.com';
    let parsed: URL;
    try { parsed = new URL(baseUrl); } catch { throw new TypeError('Spark base URL is invalid'); }
    if (parsed.protocol !== 'https:' || !parsed.hostname || (parsed.pathname !== '/' && parsed.pathname !== '') || parsed.search || parsed.hash || parsed.username || parsed.password) throw new TypeError('Spark base URL must be an HTTPS origin');
    this.baseUrl = parsed.origin;
    this.timeoutMs = config.timeoutMs ?? 45_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 131_072;
    this.model = config.model ?? 'lite';
    this.maxOutputTokens = config.maxOutputTokens ?? 512;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20) || !supportedModels.has(this.model) || !Number.isSafeInteger(this.maxOutputTokens) || this.maxOutputTokens < 1 || this.maxOutputTokens > 4096) throw new TypeError('Spark configuration is invalid');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the nonstreaming chat Query. */
  createChatCompletion(): CreateChatCompletionOperation { return new CreateChatCompletionOperation(this); }
}

/** Generate one bounded nonstreaming Spark answer. */
export class CreateChatCompletionOperation implements Query<CreateChatCompletionInput, ChatCompletion> {
  /** Bind an authenticated Spark client. */
  constructor(private readonly client: IFLYTEKClient) {}

  /** Return the stable generated Query contract. */
  definition(): QueryDefinition { return CreateChatCompletionDefinition; }

  /** Send and classify one Spark chat request. */
  async invoke(call: Call, input: CreateChatCompletionInput): Promise<QueryAttempt<ChatCompletion>> {
    if (!input || typeof input.prompt !== 'string' || !input.prompt.trim() || Array.from(input.prompt).length > 2000) return branch('defect', 'VALIDATION', 'Spark prompt must contain 1 to 2000 UTF-8 characters');
    let credentials: Credentials;
    try { credentials = await this.client.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return branch('defect', 'AUTHENTICATION', 'Spark APIPassword is unavailable or invalid'); }
    const password = credentials.apiPassword instanceof SecretString ? credentials.apiPassword.reveal() : '';
    if (!password || /[\r\n ]/.test(password)) return branch('defect', 'AUTHENTICATION', 'Spark APIPassword is unavailable or invalid');
    const body = new TextEncoder().encode(JSON.stringify({ model: this.client.model, messages: [{ role: 'user', content: input.prompt }], stream: false, max_tokens: this.client.maxOutputTokens }));
    let response: ProviderResponse;
    try {
      response = await this.client.send({ url: `${this.client.baseUrl}/v1/chat/completions`, method: 'POST', headers: { Authorization: `Bearer ${password}`, 'Content-Type': 'application/json', Accept: 'application/json' }, body, timeoutMs: this.client.timeoutMs, maxResponseBytes: this.client.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal });
    } catch (error) {
      if (error instanceof BodyTooLargeError && error.status === 200) return branch('invalidResponse', 'RESPONSE_TOO_LARGE', 'Spark response exceeded the configured limit');
      if (error instanceof BodyTooLargeError && error.status !== undefined) return classifyStatus(error.status, error.headers ?? {});
      return retry('TRANSPORT', 'Spark request failed');
    }
    if (response.status !== 200) return classifyStatus(response.status, response.headers);
    let document: unknown;
    try { document = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(response.body)) as unknown; }
    catch { return branch('invalidResponse', 'PROTOCOL', 'Spark returned invalid JSON'); }
    if (!isRecord(document) || !Number.isSafeInteger(document.code)) return branch('invalidResponse', 'PROTOCOL', 'Spark omitted code');
    const code = document.code as number;
    if (code === 10007 || code === 11202 || code === 11203) return retry('RATE_LIMIT', `Spark rate limited generation (${code})`, retryAfter(response.headers));
    if (code !== 0) return branch('providerRejected', 'PROVIDER_REJECTION', `Spark rejected generation (${code})`);
    if (!Array.isArray(document.choices) || document.choices.length !== 1 || !isRecord(document.choices[0]) || !isRecord(document.choices[0].message)) return branch('invalidResponse', 'PROTOCOL', 'Spark returned invalid assistant content');
    const message = document.choices[0].message;
    if (message.role !== 'assistant' || typeof message.content !== 'string' || !message.content || new TextEncoder().encode(message.content).length > 65_536) return branch('invalidResponse', 'PROTOCOL', 'Spark returned invalid assistant content');
    if (!isRecord(document.usage) || ![document.usage.prompt_tokens, document.usage.completion_tokens, document.usage.total_tokens].every(value => Number.isSafeInteger(value) && (value as number) >= 0)) return branch('invalidResponse', 'PROTOCOL', 'Spark returned invalid token usage');
    return newQueryBranch('completed', { content: message.content, model: this.client.model, promptTokens: document.usage.prompt_tokens as number, completionTokens: document.usage.completion_tokens as number, totalTokens: document.usage.total_tokens as number });
  }
}

function classifyStatus(status: number, headers: ProviderResponse['headers']): QueryAttempt<ChatCompletion> {
  if (status === 429) return retry('RATE_LIMIT', 'Spark rate limited the request', retryAfter(headers));
  if (status === 408 || status >= 500) return retry('AVAILABILITY', 'Spark is temporarily unavailable', retryAfter(headers));
  return branch('providerRejected', 'PROVIDER_REJECTION', `Spark rejected HTTP ${status}`);
}
function retryAfter(headers: ProviderResponse['headers']): number { const value = headers['retry-after']; return parseRetryAfter(Array.isArray(value) ? value[0] ?? '' : value ?? ''); }
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'iflytek', operation: 'createChatCompletion', kind, message }; }
function branch(id: string, kind: Failure['kind'], message: string): QueryAttempt<ChatCompletion> { return newQueryBranch(id, emptyCompletion, {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delayMs = 0): QueryAttempt<ChatCompletion> { return newQueryRetry(failure(kind, message), delayMs); }
