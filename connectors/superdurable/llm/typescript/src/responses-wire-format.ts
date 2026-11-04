// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { readErrorTokens } from '@superdurable/connectors-sdk/providerhttp';
import {
  blockedOutcome, ProviderReportedError, providerRejectedOutcome, quotaExhaustedOutcome,
  retryOutcome, temperatureNotAccepted, temperatureRange,
  type DecodedResponse, type ModelRequestRules, type WireFormat,
} from '@superdurable/connectors-sdk/textgen';

type JSONRecord = Record<string, unknown>;
const allEfforts = ['none', 'minimal', 'low', 'medium', 'high', 'xhigh', 'max'];
const efforts = (...allowed: string[]) => Object.fromEntries(allowed.map(effort => [effort, effort]));
const quota = (token: string) => ({ statusCode: 429, errorToken: token, outcome: quotaExhaustedOutcome() });
const blocked = (token: string) => ({ statusCode: 0, errorToken: token, outcome: blockedOutcome() });
const retry = (token: string, kind: 'AVAILABILITY' | 'RATE_LIMIT') =>
  ({ statusCode: 0, errorToken: token, outcome: retryOutcome(kind) });
const snapshot = '(-[0-9]{4}-[0-9]{2}-[0-9]{2})?';

interface Family {
  pattern: RegExp;
  allowed: string[];
  noTemperature?: boolean;
  temperatureOnlyWithoutReasoning?: boolean;
  reasoningOffByDefault?: boolean;
  noStreaming?: boolean;
  fineTuned?: boolean;
}
const families: Family[] = [
  { pattern: new RegExp('^gpt-6-astra' + snapshot + '$'), allowed: ['low', 'medium', 'high', 'xhigh', 'max'], noTemperature: true },
  { pattern: new RegExp('^gpt-6-(sol|luna)' + snapshot + '$'), allowed: ['none', 'low', 'medium', 'high', 'xhigh', 'max'], temperatureOnlyWithoutReasoning: true },
  { pattern: new RegExp('^gpt-5\\.6-(sol|terra|luna)' + snapshot + '$'), allowed: ['none', 'low', 'medium', 'high', 'xhigh', 'max'] },
  { pattern: new RegExp('^gpt-5\\.5-pro' + snapshot + '$'), allowed: ['medium', 'high', 'xhigh'], noStreaming: true },
  { pattern: new RegExp('^gpt-5\\.5' + snapshot + '$'), allowed: ['none', 'low', 'medium', 'high', 'xhigh'] },
  { pattern: new RegExp('^gpt-5\\.[24]-pro' + snapshot + '$'), allowed: ['medium', 'high', 'xhigh'] },
  { pattern: new RegExp('^gpt-5\\.[23]-codex' + snapshot + '$'), allowed: ['low', 'medium', 'high', 'xhigh'] },
  { pattern: new RegExp('^gpt-5\\.(2|4|4-mini|4-nano)' + snapshot + '$'), allowed: ['none', 'low', 'medium', 'high', 'xhigh'], temperatureOnlyWithoutReasoning: true, reasoningOffByDefault: true },
  { pattern: new RegExp('^gpt-5\\.1' + snapshot + '$'), allowed: ['none', 'low', 'medium', 'high'] },
  { pattern: new RegExp('^gpt-5-pro' + snapshot + '$'), allowed: ['high'] },
  { pattern: new RegExp('^gpt-5(-mini|-nano)?' + snapshot + '$'), allowed: ['minimal', 'low', 'medium', 'high'] },
  { pattern: new RegExp('^o[13]-pro' + snapshot + '$'), allowed: allEfforts, noStreaming: true },
  { pattern: /^ft:.+$/, allowed: allEfforts, fineTuned: true },
];

function familyFor(model: string): Family {
  return families.find(family => family.pattern.test(model)) ?? { pattern: /^/, allowed: allEfforts };
}

function object(value: unknown): JSONRecord {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new TypeError('response object is invalid');
  return value as JSONRecord;
}

function parse(body: Uint8Array | string): JSONRecord {
  return object(JSON.parse(typeof body === 'string' ? body : Buffer.from(body).toString('utf8')) as unknown);
}

function string(value: unknown): string {
  if (value === undefined || value === null) return '';
  if (typeof value !== 'string') throw new TypeError('response string field is invalid');
  return value;
}

function count(value: unknown): number {
  if (value === undefined || value === null) return 0;
  if (typeof value !== 'number' || !Number.isSafeInteger(value)) throw new TypeError('response count is invalid');
  return value;
}

function decodeResponse(response: JSONRecord, streamedText = ''): DecodedResponse {
  const usage = response.usage === undefined || response.usage === null ? {} : object(response.usage);
  const inputDetails = usage.input_tokens_details === undefined || usage.input_tokens_details === null ? {} : object(usage.input_tokens_details);
  const outputDetails = usage.output_tokens_details === undefined || usage.output_tokens_details === null ? {} : object(usage.output_tokens_details);
  const decoded: DecodedResponse = {
    parts: [], servedModel: string(response.model), responseId: string(response.id),
    providerFinishReason: string(response.status) === 'incomplete'
      ? string(response.incomplete_details && object(response.incomplete_details).reason) || 'incomplete'
      : string(response.status),
    usage: {
      inputTokens: count(usage.input_tokens), cachedInputTokens: count(inputDetails.cached_tokens),
      outputTokens: count(usage.output_tokens), reasoningTokens: count(outputDetails.reasoning_tokens),
      totalTokens: count(usage.total_tokens),
    },
  };
  let hasMessageContent = false;
  let text = '';
  for (const item of Array.isArray(response.output) ? response.output : []) {
    const output = object(item);
    if (output.type === 'reasoning') continue;
    for (const contentItem of Array.isArray(output.content) ? output.content : []) {
      const content = object(contentItem);
      if (content.type === 'output_text') { hasMessageContent = true; text += string(content.text); }
      if (content.type === 'refusal') { hasMessageContent = true; decoded.isRefusal = true; }
    }
  }
  if (!hasMessageContent) text = streamedText;
  if (text) decoded.parts.push({ text });
  return decoded;
}

/** OpenAI Responses dialect of the shared text generation Query. */
export function newResponsesWireFormat(): WireFormat {
  const pointers = ['/error/type', '/error/code'];
  return {
    providerName: 'openai', modelIDRule: 'body',
    features: {
      supportsInstructions: true, supportsStructuredOutput: true, supportsMaxOutputTokens: true,
      supportsTemperature: true, supportsReasoningEffort: true,
    },
    credentialHeader: { name: 'Authorization', prefix: 'Bearer ' },
    rulesForModel(model): ModelRequestRules {
      const family = familyFor(model);
      return {
        temperature: family.noTemperature ? temperatureNotAccepted() : temperatureRange(0, 2),
        reasoningEfforts: efforts(...family.allowed),
        structuredOutput: {
          mode: 'jsonSchema',
          ...(family.fineTuned ? { keywordsMovedToDescription: ['minimum', 'maximum', 'minLength', 'maxLength', 'minItems', 'maxItems', 'format'] } : {}),
        },
      };
    },
    encodeRequest({ request, rules }) {
      const family = familyFor(request.model ?? '');
      if (request.temperature !== undefined && family.temperatureOnlyWithoutReasoning &&
        request.reasoningEffort !== 'none' && !(request.reasoningEffort === undefined && family.reasoningOffByDefault)) {
        throw new TypeError('the model accepts a temperature only with reasoning effort none');
      }
      const body: JSONRecord = {
        model: request.model,
        input: request.messages.map(message => ({ role: message.role, content: message.text })),
        store: false,
      };
      if (request.instructions) body.instructions = request.instructions;
      if (request.maxOutputTokens) body.max_output_tokens = request.maxOutputTokens;
      if (request.temperature !== undefined) body.temperature = request.temperature;
      if (request.reasoningEffort) body.reasoning = { effort: rules.reasoningEfforts[request.reasoningEffort] };
      if (request.structuredOutput) body.text = { format: {
        type: 'json_schema', name: request.structuredOutput.name,
        ...(request.structuredOutput.description ? { description: request.structuredOutput.description } : {}),
        schema: request.structuredOutput.schema, strict: true,
      } };
      if (!family.noStreaming) body.stream = true;
      return { path: '/responses', body: Buffer.from(JSON.stringify(body)), isStreaming: !family.noStreaming };
    },
    decodeResponse(body) {
      const response = parse(body);
      if (response.error !== undefined && response.error !== null) throw new ProviderReportedError(readErrorTokens(body, pointers));
      return decodeResponse(response);
    },
    decodeStreamEvent(event, state, writeTextDelta) {
      const streamed = parse(event.data);
      const type = string(streamed.type);
      if (type === 'response.output_text.delta') {
        const delta = string(streamed.delta);
        state.response.parts.push({ text: delta });
        writeTextDelta(delta);
      } else if (type === 'response.completed' || type === 'response.incomplete') {
        const prior = state.response.parts.map(part => part.text).join('');
        state.response = decodeResponse(object(streamed.response), prior);
        state.hasFinished = true;
        state.isDone = true;
      } else if (type === 'response.failed') {
        throw new ProviderReportedError(readErrorTokens(Buffer.from(event.data), ['/response/error/code']));
      } else if (type === 'error') {
        throw new ProviderReportedError(readErrorTokens(Buffer.from(event.data), ['/code', '/error/type', '/error/code']));
      }
    },
    finishReasons: { completed: 'stop', max_output_tokens: 'length', content_filter: 'contentPolicy' },
    errorRules: [
      ...['insufficient_quota', 'credit_balance_exhausted', 'organization_spend_limit_exceeded',
        'project_spend_limit_exceeded', 'organization_usage_limit_exceeded'].map(quota),
      ...['misalignment_policy_violation', 'bio_policy', 'cyber_policy'].map(blocked),
      { statusCode: 501, outcome: providerRejectedOutcome('PROVIDER_REJECTION') },
      retry('server_error', 'AVAILABILITY'), retry('rate_limit_exceeded', 'RATE_LIMIT'),
    ],
    errorTokenPointers: pointers,
    requestIDHeaders: ['x-request-id'],
    rateLimitHeaders: [
      'x-ratelimit-limit-requests', 'x-ratelimit-remaining-requests', 'x-ratelimit-reset-requests',
      'x-ratelimit-limit-tokens', 'x-ratelimit-remaining-tokens', 'x-ratelimit-reset-tokens',
    ],
  };
}
