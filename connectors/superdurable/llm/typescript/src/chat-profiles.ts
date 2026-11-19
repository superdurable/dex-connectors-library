// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newWireFormat, type Profile } from '@superdurable/connectors-sdk/textgen/openaichat';
import {
  blockedOutcome, providerRejectedOutcome, quotaExhaustedOutcome, retryOutcome,
  temperatureNotAccepted, temperatureRange, type WireFormat,
} from '@superdurable/connectors-sdk/textgen';

const schema = { mode: 'jsonSchema' as const };
const jsonObject = { mode: 'jsonObjectWithInstruction' as const };
const selectedReasoning = (...values: string[]) => Object.fromEntries(values.map(value => [value, value]));
const availability = (errorToken: string) => ({ statusCode: 0, errorToken, outcome: retryOutcome('AVAILABILITY') });
const rateLimit = (errorToken: string) => ({ statusCode: 0, errorToken, outcome: retryOutcome('RATE_LIMIT') });
const quota = (errorToken: string, statusCode = 0) => ({ statusCode, errorToken, outcome: quotaExhaustedOutcome() });
const block = (errorToken: string, statusCode = 0) => ({ statusCode, errorToken, outcome: blockedOutcome() });
const preserve501 = { statusCode: 501, outcome: providerRejectedOutcome('PROVIDER_REJECTION') };

const profiles: Record<string, Profile> = {
  'openai-compatible': {
    providerName: 'openai-compatible', chatCompletionsPath: '/chat/completions',
    instructionsRole: 'system', maxTokensField: 'max_tokens',
    temperature: temperatureRange(0, 2), structuredOutput: jsonObject, streaming: 'always',
  },
  qwen: {
    providerName: 'qwen', instructionsRole: 'system', maxTokensField: 'max_tokens',
    temperature: temperatureRange(0, 2 - Number.EPSILON), structuredOutput: jsonObject,
    streaming: 'always', shouldRequestStreamUsage: true,
    errorRules: [
      block('data_inspection_failed'), block('DataInspectionFailed'),
      quota('Arrearage', 400), quota('AllocationQuota.FreeTierOnly', 403),
      ...['CommodityNotPurchased', 'PrepaidBillOverdue', 'PostpaidBillOverdue', 'BudgetLimitExceeded'].map(token => quota(token, 429)),
    ],
    modelRules: [
      { modelIDPattern: '^qwen3\\.8-(?:max|max-0902|flash)$', reasoningEfforts: selectedReasoning('none', 'low', 'medium', 'xhigh'), maxTokensField: 'max_completion_tokens' },
      { modelIDPattern: '^qwen3\\.8-27b$', reasoningEfforts: selectedReasoning('none', 'low', 'medium', 'xhigh') },
      { modelIDPattern: '^qwen3\\.8-2\\.4t-a95b$', reasoningEfforts: selectedReasoning('low', 'medium', 'xhigh') },
      { modelIDPattern: '^qwen3\\.[5-9]-(?:plus|flash)(?:-\\d{4}-\\d{2}-\\d{2})?$', maxTokensField: 'max_completion_tokens' },
      { modelIDPattern: '^qwen3\\.[7-9]-max(?:-preview|-\\d{4}-\\d{2}-\\d{2})?$', maxTokensField: 'max_completion_tokens' },
    ],
  },
  deepseek: {
    providerName: 'deepseek', temperature: temperatureNotAccepted(), structuredOutput: jsonObject,
    reasoningEfforts: selectedReasoning('none', 'low', 'high', 'max'), streaming: 'always',
    requestIDHeaders: ['x-ds-trace-id'], stallTimeoutMs: 300_000,
  },
  meta: {
    providerName: 'meta', chatCompletionsPath: '/v1/chat/completions', instructionsRole: 'developer',
    maxTokensField: 'max_completion_tokens', temperature: temperatureRange(0, 2),
    reasoningEfforts: selectedReasoning('minimal', 'low', 'medium', 'high', 'xhigh'),
    structuredOutput: schema, shouldSendStrictJSONSchema: true, streaming: 'always', shouldRequestStreamUsage: true,
    errorRules: [
      block('content_policy_violation', 400), preserve501,
      ...['server_error', 'server_shutting_down', 'service_overloaded', 'backend_unavailable'].map(availability),
      rateLimit('rate_limit_error'), rateLimit('rate_limit_exceeded'),
    ],
    rateLimitHeaders: ['x-ratelimit-limit-requests', 'x-ratelimit-remaining-requests', 'x-ratelimit-limit-tokens', 'x-ratelimit-remaining-tokens'],
    modelRules: [{ modelIDPattern: '^muse-spark-1\\.3$', reasoningEfforts: selectedReasoning('minimal', 'low', 'medium', 'high', 'xhigh', 'max') }],
  },
  mistral: {
    providerName: 'mistral', temperature: temperatureRange(0, 1.5), structuredOutput: schema,
    shouldSendStrictJSONSchema: true, streaming: 'always',
    allowedRequestFields: ['model', 'messages', 'max_tokens', 'temperature', 'reasoning_effort', 'response_format', 'stream'],
    finishReasons: { model_length: 'length' }, errorTokenPointers: ['/type', '/code'],
    requestIDHeaders: ['mistral-correlation-id'], rateLimitHeaders: ['x-ratelimit-remaining'],
    modelRules: [
      { modelIDPattern: '^mistral-small-(2603|latest)$', reasoningEfforts: selectedReasoning('none', 'high') },
      { modelIDPattern: '^mistral-medium-(3|3-5|latest)$', reasoningEfforts: selectedReasoning('none', 'high') },
    ],
  },
  kimi: {
    providerName: 'kimi', maxTokensField: 'max_completion_tokens', temperature: temperatureNotAccepted(),
    structuredOutput: {
      mode: 'jsonSchema', keywordsMovedToDescription: ['minimum', 'maximum', 'minLength', 'maxLength', 'minItems', 'maxItems', 'format'],
      keywordsRemoved: ['title'],
    },
    shouldSendStrictJSONSchema: true, streaming: 'always', shouldRequestStreamUsage: true,
    errorRules: [
      preserve501, block('content_filter'), quota('exceeded_current_quota_error'),
      availability('engine_overloaded_error'), rateLimit('rate_limit_reached_error'),
      ...['server_error', 'unexpected_output', 'server_unavailable'].map(availability),
    ],
    modelRules: [{ modelIDPrefix: 'kimi-k3', reasoningEfforts: selectedReasoning('low', 'high', 'max') }],
  },
  xai: {
    providerName: 'xai', maxTokensField: 'max_completion_tokens',
    reasoningEfforts: selectedReasoning('low', 'medium', 'high', 'xhigh'),
    temperature: temperatureRange(0, 2), structuredOutput: schema, shouldSendStrictJSONSchema: true,
    streaming: 'always', shouldRequestStreamUsage: true, finishReasons: { end_turn: 'stop' },
    errorTokenPointers: ['/code'],
    modelRules: [
      { modelIDPattern: '^grok-4\\.3(-latest)?$', reasoningEfforts: selectedReasoning('none', 'low', 'medium', 'high', 'xhigh') },
      { modelIDPattern: '^(grok-4\\.5|grok-4\\.5-latest|grok-build-latest)$', reasoningEfforts: selectedReasoning('low', 'medium', 'high') },
      { modelIDPattern: '^grok-4\\.20(-.+)?$', reasoningEfforts: {} },
      { modelIDPattern: '^(grok-build-0\\.1|grok-code-fast|grok-code-fast-1|grok-code-fast-1-0825)$', reasoningEfforts: {} },
    ],
  },
};

/** Return the provider's Chat Completions dialect with Go's model-specific rules. */
export function newChatWireFormat(provider: string): WireFormat {
  const profile = profiles[provider];
  if (!profile) throw new TypeError('provider does not use Chat Completions');
  const format = newWireFormat(profile);
  if (provider !== 'xai') return format;
  const countReasoning = (usage: NonNullable<ReturnType<NonNullable<WireFormat['decodeResponse']>>['usage']>) => {
    const reasoning = usage.reasoningTokens ?? 0;
    if (reasoning > 0 && usage.outputTokens !== undefined &&
      (usage.totalTokens ?? 0) !== (usage.inputTokens ?? 0) + usage.outputTokens) {
      usage.outputTokens += reasoning;
    }
  };
  const decodeResponse = format.decodeResponse;
  const decodeStreamEvent = format.decodeStreamEvent;
  return {
    ...format,
    ...(decodeResponse ? { decodeResponse: (body: Uint8Array) => {
      const response = decodeResponse(body);
      if (response.usage) countReasoning(response.usage);
      return response;
    } } : {}),
    ...(decodeStreamEvent ? { decodeStreamEvent: (...args: Parameters<NonNullable<WireFormat['decodeStreamEvent']>>) => {
      decodeStreamEvent(...args);
      if (args[1].isDone && args[1].response.usage) countReasoning(args[1].response.usage);
    } } : {}),
  };
}
