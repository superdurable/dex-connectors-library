// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GenerateTextDefinition } from './generated/definitions.js';
import type { Config, Credentials, GenerateTextInput, GenerateTextOutput } from './generated/models.js';

const emptyOutput: GenerateTextOutput = { text: '', model: '', responseId: '', finishReason: '', usage: { promptTokens: 0, completionTokens: 0, totalTokens: 0 } };
/** Optional provider transport for deterministic tests. */
export interface Qihoo360ClientOptions { /** Sender; production uses Connector SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>; }
/** Resolve rotated credentials and call Qihoo360's nonstreaming chat API. */
export class Qihoo360Client {
  private readonly model: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Validate settings without contacting the provider. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: Qihoo360ClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Qihoo360 configuration and credential source are required');
    this.model = config.model || '360zhinao/360zhinao-pro';
    this.timeoutMs = config.timeoutMs ?? 120_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 2_097_152;
    if (!/^[A-Za-z0-9._/-]{1,128}$/.test(this.model) || !Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('Qihoo360 model, timeout, or response bound is invalid');
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the text generation Query. */ generateText(): GenerateTextOperation { return new GenerateTextOperation(this); }
  /** Send one bounded request and classify transport separately from provider response. */
  async request(call: Call, input: GenerateTextInput): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge' | 'input'; retryAfterMs: number }> {
    const prompt = input?.prompt;
    const tokens = input?.maxOutputTokens ?? 0;
    if (typeof prompt !== 'string' || !prompt.trim() || Buffer.byteLength(prompt, 'utf8') > (64 << 10) || !Number.isInteger(tokens) || tokens < 0 || tokens > 4096) return { error: 'input', retryAfterMs: 0 };
    let credentials: Credentials;
    try { credentials = await this.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return { error: 'credential', retryAfterMs: 0 }; }
    const key = credentials.apiKey instanceof SecretString ? credentials.apiKey.reveal() : '';
    if (!key || key.length > 512 || !/^[!-~]+$/.test(key)) return { error: 'credential', retryAfterMs: 0 };
    const body = Buffer.from(JSON.stringify({ model: this.model, messages: [{ role: 'user', content: prompt }], stream: false, max_tokens: tokens || 1024 }));
    const request: ProviderRequest = {
      url: 'https://api.360.cn/v1/chat/completions', method: 'POST',
      headers: { Authorization: `Bearer ${key}`, 'Content-Type': 'application/json', Accept: 'application/json' },
      body, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes,
      addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal,
    };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    const retryAfterHeader = Object.entries(response.headers).find(([name]) => name.toLowerCase() === 'retry-after')?.[1];
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(Array.isArray(retryAfterHeader) ? retryAfterHeader[0] ?? '' : retryAfterHeader ?? '') };
  }
}
/** Classify Qihoo360 completions without persisting credentials or raw provider payloads. */
export class GenerateTextOperation implements Query<GenerateTextInput, GenerateTextOutput> {
  /** Bind one validated client. */ constructor(private readonly client: Qihoo360Client) {}
  /** Return the generated operation contract. */ definition(): QueryDefinition { return GenerateTextDefinition; }
  /** Invoke a safe Query once. */
  async invoke(call: Call, input: GenerateTextInput): Promise<QueryAttempt<GenerateTextOutput>> {
    const exchange = await this.client.request(call, input);
    if (exchange.error === 'input') return branch('defect', 'VALIDATION');
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    let data: unknown;
    try { data = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; }
    catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (isRecord(data) && Number.isInteger(data.code) && data.code !== 0) {
      if (data.code === 1003) return retry('AVAILABILITY', exchange.retryAfterMs);
      if (data.code === 1005 || data.code === 10133) return retry('RATE_LIMIT', exchange.retryAfterMs);
      return branch('providerRejected', data.code === 1002 ? 'AUTHENTICATION' : 'PROVIDER_REJECTION');
    }
    if (!isRecord(data) || typeof data.id !== 'string' || !data.id || typeof data.model !== 'string' || !data.model || !Array.isArray(data.choices) || data.choices.length !== 1 || !isRecord(data.choices[0]) || !isRecord(data.choices[0].message)) return branch('invalidResponse', 'PROTOCOL');
    const choice = data.choices[0];
    const message = choice.message as Record<string, unknown>;
    const usage = isRecord(data.usage) ? data.usage : { prompt_tokens: 0, completion_tokens: 0, total_tokens: 0 };
    if (message.role !== 'assistant' || message.tool_calls !== undefined && message.tool_calls !== null || ![usage.prompt_tokens, usage.completion_tokens, usage.total_tokens].every(value => typeof value === 'number' && Number.isSafeInteger(value) && value >= 0)) return branch('invalidResponse', 'PROTOCOL');
    if (message.content !== undefined && message.content !== null && typeof message.content !== 'string') return branch('invalidResponse', 'PROTOCOL');
    const finishReason = typeof choice.finish_reason === 'string' ? choice.finish_reason : '';
    const text = typeof message.content === 'string' ? message.content : '';
    if (!['stop', '', 'length', 'sensitive', 'content_filter'].includes(finishReason) || (finishReason === 'stop' || finishReason === '') && !text) return branch('invalidResponse', 'PROTOCOL');
    const output: GenerateTextOutput = { text, model: data.model, responseId: data.id, finishReason, usage: { promptTokens: usage.prompt_tokens as number, completionTokens: usage.completion_tokens as number, totalTokens: usage.total_tokens as number } };
    if (finishReason === 'sensitive' || finishReason === 'content_filter') return newQueryBranch('blocked', output, {}, failure('PROVIDER_REJECTION'));
    return newQueryBranch(finishReason === 'length' ? 'truncated' : 'generated', output);
  }
}
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function failure(kind: Failure['kind']): Failure { return { provider: 'qihoo360', operation: 'generateText', kind, message: 'Qihoo360 text generation failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<GenerateTextOutput> { return newQueryBranch(name, emptyOutput, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<GenerateTextOutput> { return newQueryRetry(failure(kind), delay); }
