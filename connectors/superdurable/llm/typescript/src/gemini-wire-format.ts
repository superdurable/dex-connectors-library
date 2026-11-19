// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { readErrorTokens } from '@superdurable/connectors-sdk/providerhttp';
import {
  ProviderReportedError, providerRejectedOutcome, temperatureRange,
  type DecodedResponse, type ModelRequestRules, type WireFormat,
} from '@superdurable/connectors-sdk/textgen';

type JSONRecord = Record<string, unknown>;
const pointers = [
  '/error/status', '/error/details/0/reason', '/error/details/1/reason', '/error/details/2/reason',
  '/0/error/status', '/0/error/details/0/reason', '/0/error/details/1/reason', '/0/error/details/2/reason',
];
const contentPolicyReasons = [
  'SAFETY', 'RECITATION', 'LANGUAGE', 'BLOCKLIST', 'PROHIBITED_CONTENT', 'SPII',
  'IMAGE_SAFETY', 'IMAGE_PROHIBITED_CONTENT', 'IMAGE_RECITATION', 'IMAGE_OTHER',
  'ESCALATION', 'PUP_LIMITED_DISABLED',
];
const promptBlockReasons = ['SAFETY', 'OTHER', 'BLOCKLIST', 'PROHIBITED_CONTENT', 'IMAGE_SAFETY'];
const levels = { minimal: 'MINIMAL', low: 'LOW', medium: 'MEDIUM', high: 'HIGH' };

function object(value: unknown): JSONRecord {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new TypeError('Gemini response object is invalid');
  return value as JSONRecord;
}
function string(value: unknown): string {
  if (value === null || value === undefined) return '';
  if (typeof value !== 'string') throw new TypeError('Gemini response string is invalid');
  return value;
}
function count(value: unknown): number {
  if (value === null || value === undefined) return 0;
  if (typeof value !== 'number' || !Number.isSafeInteger(value)) throw new TypeError('Gemini token count is invalid');
  return value;
}

function rulesForModel(model: string): ModelRequestRules {
  const reasoningEfforts = /^gemini-[12]\./.test(model) ? {}
    : (/^gemini-3\.[78]-flash|^gemini-3(\.1)?-pro/.test(model) && !model.includes('-lite') ||
      /^gemini-(flash|pro)-latest$/.test(model))
      ? { low: 'LOW', medium: 'MEDIUM', high: 'HIGH' }
      : levels;
  return {
    temperature: temperatureRange(0, 2), reasoningEfforts,
    structuredOutput: { mode: 'jsonSchema', keywordsMovedToDescription: ['minLength', 'maxLength', 'format'] },
  };
}

function decode(body: Uint8Array): DecodedResponse {
  const parsed = JSON.parse(Buffer.from(body).toString('utf8')) as unknown;
  const response = object(parsed);
  if (response.error !== undefined && response.error !== null) throw new ProviderReportedError(readErrorTokens(body, pointers));
  const usage = response.usageMetadata ? object(response.usageMetadata) : {};
  const thoughtCount = count(usage.thoughtsTokenCount);
  const result: DecodedResponse = {
    parts: [], servedModel: string(response.modelVersion), responseId: string(response.responseId),
    usage: {
      inputTokens: count(usage.promptTokenCount), cachedInputTokens: count(usage.cachedContentTokenCount),
      outputTokens: count(usage.candidatesTokenCount) + thoughtCount, reasoningTokens: thoughtCount,
      totalTokens: count(usage.totalTokenCount),
    },
  };
  const feedback = response.promptFeedback ? object(response.promptFeedback) : {};
  const blockReason = string(feedback.blockReason);
  if (blockReason && blockReason !== 'BLOCK_REASON_UNSPECIFIED') {
    result.providerFinishReason = promptBlockReasons.includes(blockReason)
      ? 'blockReason:' + blockReason
      : /^[A-Z][A-Z0-9_]{0,63}$/.test(blockReason) ? 'blockReason:UNDOCUMENTED' : '';
    return result;
  }
  const candidates = response.candidates;
  if (!Array.isArray(candidates) || candidates.length === 0) return result;
  const candidate = object(candidates[0]);
  result.providerFinishReason = string(candidate.finishReason);
  const content = candidate.content ? object(candidate.content) : {};
  for (const value of Array.isArray(content.parts) ? content.parts : []) {
    const part = object(value);
    result.parts.push({ text: string(part.text), ...(part.thought === true ? { isReasoning: true } : {}) });
  }
  return result;
}

function readRetryDelay(body: Uint8Array): number {
  let decoded: unknown;
  try { decoded = JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { return 0; }
  const envelope = Array.isArray(decoded) ? decoded[0] : decoded;
  if (!envelope || typeof envelope !== 'object') return 0;
  const error = (envelope as JSONRecord).error;
  if (!error || typeof error !== 'object') return 0;
  const details = (error as JSONRecord).details;
  if (!Array.isArray(details)) return 0;
  let delay = 0;
  for (const value of details) {
    if (!value || typeof value !== 'object') continue;
    const detail = value as JSONRecord;
    if (detail['@type'] !== 'type.googleapis.com/google.rpc.RetryInfo' || typeof detail.retryDelay !== 'string') continue;
    const match = /^([0-9]{1,10})(?:\.([0-9]{1,9}))?s$/.exec(detail.retryDelay);
    if (!match) continue;
    delay = Math.min(Number(match[1]) * 1000 + Number('0.' + (match[2] ?? '0')) * 1000, 3_600_000);
  }
  return delay;
}

/** Gemini generateContent dialect of the shared text generation Query. */
export function newGeminiWireFormat(): WireFormat {
  return {
    providerName: 'gemini', modelIDRule: 'pathSegment',
    features: {
      supportsInstructions: true, supportsStructuredOutput: true, supportsMaxOutputTokens: true,
      supportsTemperature: true, supportsReasoningEffort: true,
    },
    credentialHeader: { name: 'x-goog-api-key', prefix: '' },
    rulesForModel,
    encodeRequest({ request, rules }) {
      const body: JSONRecord = {
        contents: request.messages.map(message => ({
          role: message.role === 'assistant' ? 'model' : 'user', parts: [{ text: message.text }],
        })),
      };
      if (request.instructions) body.systemInstruction = { parts: [{ text: request.instructions }] };
      const generationConfig: JSONRecord = {};
      if (request.structuredOutput) {
        const output = request.structuredOutput;
        const responseSchema = { ...output.schema };
        if (output.description) {
          responseSchema.description = output.description +
            (typeof responseSchema.description === 'string' && responseSchema.description ? '\n\n' + responseSchema.description : '');
        }
        generationConfig.responseMimeType = 'application/json';
        generationConfig.responseJsonSchema = responseSchema;
      }
      if (request.temperature !== undefined) generationConfig.temperature = request.temperature;
      if (request.maxOutputTokens) generationConfig.maxOutputTokens = request.maxOutputTokens;
      if (request.reasoningEffort) generationConfig.thinkingConfig = { thinkingLevel: rules.reasoningEfforts[request.reasoningEffort] };
      if (Object.keys(generationConfig).length) body.generationConfig = generationConfig;
      return { path: '/models/' + request.model + ':generateContent', body: Buffer.from(JSON.stringify(body)), isStreaming: false };
    },
    decodeResponse: decode,
    finishReasons: {
      STOP: 'stop', MAX_TOKENS: 'length',
      ...Object.fromEntries(contentPolicyReasons.map(reason => [reason, 'contentPolicy'])),
      ...Object.fromEntries([...promptBlockReasons, 'UNDOCUMENTED'].map(reason => ['blockReason:' + reason, 'contentPolicy'])),
    },
    errorRules: [{ statusCode: 400, errorToken: 'API_KEY_INVALID', outcome: providerRejectedOutcome('AUTHENTICATION') }],
    errorTokenPointers: pointers,
    readErrorRetryDelay: readRetryDelay,
    requestIDHeaders: ['X-Goog-Request-Id', 'X-Request-Id'],
  };
}
