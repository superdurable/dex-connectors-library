// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GenerateTextDefinition } from './generated/definitions.js';
import type { Config, Credentials, GenerateTextInput, GenerateTextOutput } from './generated/models.js';

const endpoint = 'https://open.bigmodel.cn/api/paas/v4/chat/completions';
const emptyOutput: GenerateTextOutput = { text: '', model: '', responseId: '', finishReason: '', usage: { promptTokens: 0, completionTokens: 0, totalTokens: 0 } };
/** Sender used for Zhipu nonstreaming generation. */
export type ZhipuRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;
/** Optional transport dependency for deterministic provider tests. */
export interface ZhipuClientOptions { /** Production uses the SDK sender. */ send?: ZhipuRequestSender; }
/** Generate text with a configured GLM model and fresh credentials per call. */
export class ZhipuClient {
  readonly model: string;
  readonly timeoutMs: number;
  readonly maxResponseBytes: number;
  readonly credentials: CredentialProvider<Credentials>;
  readonly send: ZhipuRequestSender;
  /** Validate the model and bounded request settings. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: ZhipuClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Zhipu configuration and credentials are required');
    this.model = config.model || 'glm-4.7-flash';
    if (!/^[A-Za-z0-9._-]{1,128}$/.test(this.model)) throw new TypeError('Zhipu model ID is invalid');
    this.timeoutMs = config.timeoutMs ?? 120_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 2_097_152;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('Zhipu request limits must be positive');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the generation Query. */ generateText(): GenerateTextOperation { return new GenerateTextOperation(this); }
}
/** Request one bounded nonstreaming text answer. */
export class GenerateTextOperation implements Query<GenerateTextInput, GenerateTextOutput> {
  /** Bind the Query client. */ constructor(private readonly client: ZhipuClient) {}
  /** Stable manifest definition. */ definition(): QueryDefinition { return GenerateTextDefinition; }
  /** Execute a GLM chat completion and classify its finish reason. */
  async invoke(call: Call, input: GenerateTextInput): Promise<QueryAttempt<GenerateTextOutput>> {
    if (typeof input.prompt !== 'string' || !input.prompt.trim() || new TextEncoder().encode(input.prompt).length > 64 * 1024 || !Number.isSafeInteger(input.maxOutputTokens ?? 0) || (input.maxOutputTokens ?? 0) < 0 || (input.maxOutputTokens ?? 0) > 4096) return branch('defect', 'VALIDATION');
    let credentials: Credentials;
    try { credentials = await this.client.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return branch('defect', 'AUTHENTICATION'); }
    const apiKey = credentials.apiKey instanceof SecretString ? credentials.apiKey.reveal() : '';
    if (!apiKey || apiKey.length > 512 || !/^[!-~]+$/.test(apiKey)) return branch('defect', 'AUTHENTICATION');
    const body = new TextEncoder().encode(JSON.stringify({ model: this.client.model, messages: [{ role: 'user', content: input.prompt }], stream: false, max_tokens: input.maxOutputTokens || 1024, response_format: { type: 'text' } }));
    let response: ProviderResponse;
    try { response = await this.client.send({ url: endpoint, method: 'POST', headers: { Authorization: 'Bearer ' + apiKey, 'Content-Type': 'application/json', Accept: 'application/json' }, body,
      timeoutMs: this.client.timeoutMs, maxResponseBytes: this.client.maxResponseBytes,
      addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal }); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status === 200) return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
      if (error instanceof BodyTooLargeError && error.status !== undefined) return classifyStatus(error.status, error.headers ?? {});
      return retry('AVAILABILITY');
    }
    if (response.status !== 200) return classifyStatus(response.status, response.headers);
    if (response.body.length > this.client.maxResponseBytes) return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    let document: unknown;
    try { document = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(response.body)) as unknown; }
    catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(document) || typeof document.id !== 'string' || !document.id || typeof document.model !== 'string' || !document.model || !Array.isArray(document.choices) || document.choices.length !== 1) return branch('invalidResponse', 'PROTOCOL');
    const choice: unknown = document.choices[0];
    if (!isRecord(choice) || !isRecord(choice.message) || choice.message.role !== 'assistant' || (choice.message.tool_calls !== undefined && choice.message.tool_calls !== null)) return branch('invalidResponse', 'PROTOCOL');
    const usage = document.usage === undefined ? {} : document.usage;
    if (!isRecord(usage)) return branch('invalidResponse', 'PROTOCOL');
    const promptTokens = usage.prompt_tokens ?? 0;
    const completionTokens = usage.completion_tokens ?? 0;
    const totalTokens = usage.total_tokens ?? 0;
    if (![promptTokens, completionTokens, totalTokens].every(value => Number.isSafeInteger(value) && (value as number) >= 0)) return branch('invalidResponse', 'PROTOCOL');
    if (choice.message.content !== undefined && choice.message.content !== null && typeof choice.message.content !== 'string') return branch('invalidResponse', 'PROTOCOL');
    const output: GenerateTextOutput = { text: typeof choice.message.content === 'string' ? choice.message.content : '', model: document.model, responseId: document.id,
      finishReason: typeof choice.finish_reason === 'string' ? choice.finish_reason : '', usage: { promptTokens: promptTokens as number, completionTokens: completionTokens as number, totalTokens: totalTokens as number } };
    if (output.finishReason === 'stop' && output.text) return newQueryBranch('generated', output);
    if (output.finishReason === 'length') return newQueryBranch('truncated', output);
    if (output.finishReason === 'sensitive' || output.finishReason === 'content_filter') return newQueryBranch('blocked', output, {}, failure('PROVIDER_REJECTION'));
    return branch('invalidResponse', 'PROTOCOL');
  }
}
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function classifyStatus(status: number, headers: ProviderResponse['headers']): QueryAttempt<GenerateTextOutput> {
  const value = headers['retry-after']; const delay = parseRetryAfter(Array.isArray(value) ? value[0] ?? '' : value ?? '');
  if (status === 429) return retry('RATE_LIMIT', delay);
  if (status === 408 || status >= 500) return retry('AVAILABILITY', delay);
  return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
}
function failure(kind: Failure['kind']): Failure { return { provider: 'zhipu', operation: 'generateText', kind, message: 'Zhipu text generation failed' }; }
function branch(id: string, kind: Failure['kind']): QueryAttempt<GenerateTextOutput> { return newQueryBranch(id, emptyOutput, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<GenerateTextOutput> { return newQueryRetry(failure(kind), delay); }
