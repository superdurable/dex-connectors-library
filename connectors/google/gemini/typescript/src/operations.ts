// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newQueryBranch, newQueryRetry, type Call, type Failure, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import { BodyTooLargeError } from '@superdurable/connectors-sdk/providerhttp';
import { GeminiClient, GeminiCredentialError, retryDelay } from './client.js';
import { GenerateContentDefinition } from './generated/definitions.js';
import type { GenerateContentRequest, GenerateContentResponse, Usage } from './generated/models.js';

const zeroUsage: Usage = { promptTokens: 0, candidateTokens: 0, thoughtsTokens: 0, cachedContentTokens: 0, totalTokens: 0 };
const zeroResponse: GenerateContentResponse = { text: '', usage: zeroUsage };
const enumPattern = /^[A-Z][A-Z0-9_]{0,63}$/;
const policyReasons = new Set(['SAFETY', 'RECITATION', 'LANGUAGE', 'BLOCKLIST', 'PROHIBITED_CONTENT', 'SPII', 'IMAGE_SAFETY', 'IMAGE_PROHIBITED_CONTENT', 'IMAGE_RECITATION', 'IMAGE_OTHER', 'ESCALATION', 'PUP_LIMITED_DISABLED']);

/** One Gemini models.generateContent Query. */
export class GenerateContentOperation implements Query<GenerateContentRequest, GenerateContentResponse> {
  /** Bind a client. */ constructor(private readonly client: GeminiClient) {}
  /** Return the generated manifest definition. */ definition() { return GenerateContentDefinition; }
  /** Validate, send, and classify one generation attempt. */
  async invoke(call: Call, input: GenerateContentRequest): Promise<QueryAttempt<GenerateContentResponse>> {
    let model: string;
    let body: Uint8Array;
    try { ({ model, body } = encodeRequest(input, this.client.model)); }
    catch { return branch('defect', zeroResponse, 'VALIDATION', 'Gemini request is invalid'); }
    let response;
    try { response = await this.client.exchange(call, model, body); }
    catch (error) {
      if (error instanceof GeminiCredentialError) return branch('defect', zeroResponse, 'AUTHENTICATION', 'connection credentials are unavailable');
      if (error instanceof BodyTooLargeError) return branch('invalidResponse', zeroResponse, 'RESPONSE_TOO_LARGE', 'provider response exceeds the configured size limit');
      return newQueryRetry(failure('TRANSPORT', 'provider request failed before a response was received'));
    }
    const receipt = this.client.receipt(call, response);
    if (response.status < 200 || response.status > 299) {
      const { kind, message } = classifyStatus(response.status, response.body);
      if (response.status === 408 || response.status === 429 || (response.status >= 500 && response.status !== 501)) return newQueryRetry(failure(kind, message), retryDelay(response, this.client.now()));
      return newQueryBranch('providerRejected', zeroResponse, receipt, failure(kind, message));
    }
    let wire: unknown;
    try { wire = JSON.parse(Buffer.from(response.body).toString('utf8')) as unknown; }
    catch { return branch('invalidResponse', zeroResponse, 'PROTOCOL', 'provider response is not valid JSON', receipt); }
    if (!wire || typeof wire !== 'object' || Array.isArray(wire)) return branch('invalidResponse', zeroResponse, 'PROTOCOL', 'provider response is invalid', receipt);
    const parsed = wire as WireResponse;
    const metadata = parsed.usageMetadata ?? {};
    const value: GenerateContentResponse = {
      text: '', usage: { promptTokens: metadata.promptTokenCount ?? 0, candidateTokens: metadata.candidatesTokenCount ?? 0,
        thoughtsTokens: metadata.thoughtsTokenCount ?? 0, cachedContentTokens: metadata.cachedContentTokenCount ?? 0, totalTokens: metadata.totalTokenCount ?? 0 },
      ...(parsed.responseId ? { responseId: parsed.responseId } : {}), ...(parsed.modelVersion ? { modelVersion: parsed.modelVersion } : {}),
    };
    const outcomeReceipt = this.client.receipt(call, response, value.responseId);
    const blockReason = parsed.promptFeedback?.blockReason;
    if (blockReason && blockReason !== 'BLOCK_REASON_UNSPECIFIED') {
      if (!enumPattern.test(blockReason)) return branch('invalidResponse', value, 'PROTOCOL', 'provider returned an unrecognized block reason', outcomeReceipt);
      value.blockReason = blockReason;
      return branch('blocked', value, 'PROVIDER_REJECTION', `provider blocked the prompt: ${blockReason}`, outcomeReceipt);
    }
    const candidate = parsed.candidates?.[0];
    if (!candidate || !candidate.finishReason || !enumPattern.test(candidate.finishReason)) return branch('invalidResponse', value, 'PROTOCOL', 'provider returned no usable candidate', outcomeReceipt);
    value.finishReason = candidate.finishReason;
    const content = (candidate.content?.parts ?? []).filter(part => !part.thought).map(part => part.text ?? '').join('');
    if (candidate.finishReason === 'STOP') {
      if (!content) return branch('invalidResponse', value, 'PROTOCOL', 'provider candidate finished without text', outcomeReceipt);
      value.text = content;
      return newQueryBranch('generated', value, outcomeReceipt);
    }
    if (candidate.finishReason === 'MAX_TOKENS') { value.text = content; return branch('truncated', value, 'RESPONSE_TOO_LARGE', 'provider stopped the candidate at the output token limit', outcomeReceipt); }
    if (policyReasons.has(candidate.finishReason)) return branch('blocked', value, 'PROVIDER_REJECTION', `provider stopped the candidate: ${candidate.finishReason}`, outcomeReceipt);
    return branch('invalidResponse', value, 'PROTOCOL', `provider stopped the candidate without a usable result: ${candidate.finishReason}`, outcomeReceipt);
  }
}

interface WireResponse {
  responseId?: string; modelVersion?: string;
  promptFeedback?: { blockReason?: string };
  usageMetadata?: { promptTokenCount?: number; candidatesTokenCount?: number; thoughtsTokenCount?: number; cachedContentTokenCount?: number; totalTokenCount?: number };
  candidates?: Array<{ finishReason?: string; content?: { parts?: Array<{ text?: string; thought?: boolean }> } }>;
}

function encodeRequest(input: GenerateContentRequest, connectionModel: string): { model: string; body: Uint8Array } {
  const model = input.model ? input.model.replace(/^models\//, '') : connectionModel;
  if (!/^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/.test(model)) throw new TypeError('invalid model');
  if (!input.contents?.length) throw new TypeError('contents required');
  const contents = input.contents.map(content => {
    if (content.role && content.role !== 'user' && content.role !== 'model') throw new TypeError('invalid role');
    if (!content.parts?.length || content.parts.some(part => !part.text)) throw new TypeError('invalid parts');
    return { ...(content.role ? { role: content.role } : {}), parts: content.parts.map(part => ({ text: part.text })) };
  });
  const request: Record<string, unknown> = { contents };
  if (input.systemInstruction) request.systemInstruction = { parts: [{ text: input.systemInstruction }] };
  const generation: Record<string, unknown> = {};
  let mimeType = input.responseMimeType;
  if (input.responseJsonSchema !== undefined) {
    mimeType ||= 'application/json';
    if (mimeType !== 'application/json') throw new TypeError('schema MIME type');
    generation.responseJsonSchema = input.responseJsonSchema;
  }
  if (mimeType) generation.responseMimeType = mimeType;
  if (input.temperature !== undefined) { if (!Number.isFinite(input.temperature) || input.temperature < 0 || input.temperature > 2) throw new TypeError('temperature'); generation.temperature = input.temperature; }
  if (input.maxOutputTokens !== undefined) { if (input.maxOutputTokens < 0) throw new TypeError('maxOutputTokens'); if (input.maxOutputTokens !== 0) generation.maxOutputTokens = input.maxOutputTokens; }
  if (input.thinkingBudget !== undefined) { if (input.thinkingBudget < -1) throw new TypeError('thinkingBudget'); generation.thinkingConfig = { thinkingBudget: input.thinkingBudget }; }
  if (Object.keys(generation).length) request.generationConfig = generation;
  return { model, body: Buffer.from(JSON.stringify(request), 'utf8') };
}

function classifyStatus(status: number, body: Uint8Array): { kind: Failure['kind']; message: string } {
  let code = '';
  let reason = '';
  try {
    const parsed: unknown = JSON.parse(Buffer.from(body).toString('utf8'));
    const root = Array.isArray(parsed) ? parsed[0] : parsed;
    if (root && typeof root === 'object' && 'error' in root) {
      const error = (root as { error?: { status?: string; details?: Array<{ '@type'?: string; reason?: string }> } }).error;
      if (error?.status && enumPattern.test(error.status)) code = error.status;
      for (const detail of error?.details ?? []) if (detail['@type'] === 'type.googleapis.com/google.rpc.ErrorInfo' && detail.reason && enumPattern.test(detail.reason)) reason = detail.reason;
    }
  } catch { /* Invalid error JSON leaves the HTTP status authoritative. */ }
  const kind: Failure['kind'] = status === 401 || reason === 'API_KEY_INVALID' ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : status === 404 ? 'NOT_FOUND' : status === 409 ? 'CONFLICT' : status === 429 ? 'RATE_LIMIT' : status === 408 || (status >= 500 && status !== 501) ? 'AVAILABILITY' : 'PROVIDER_REJECTION';
  return { kind, message: `provider returned HTTP ${status}${code ? ` ${code}` : ''}` };
}

function failure(kind: Failure['kind'], message: string): Failure { return { kind, provider: 'gemini', operation: 'generateContent', message }; }
function branch(name: string, value: GenerateContentResponse, kind: Failure['kind'], message: string, receipt = {}): QueryAttempt<GenerateContentResponse> {
  return newQueryBranch(name, value, receipt, failure(kind, message));
}
