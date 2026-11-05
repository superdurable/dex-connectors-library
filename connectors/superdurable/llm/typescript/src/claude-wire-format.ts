// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { readErrorTokens } from '@superdurable/connectors-sdk/providerhttp';
import {
  ProviderReportedError, providerRejectedOutcome, quotaExhaustedOutcome, retryOutcome, temperatureRange,
  type DecodedResponse, type ModelRequestRules, type ResponsePart, type Usage, type WireFormat,
} from '@superdurable/connectors-sdk/textgen';

type JSONRecord = Record<string, unknown>;
const pointers = ['/error/type', '/error/details/error_code'];
const structuredOutput = { mode: 'jsonSchema' as const, keywordsMovedToDescription: ['minimum', 'maximum', 'minLength', 'maxLength', 'minItems', 'maxItems'] };
const effortMap = (...values: string[]) => Object.fromEntries(values.map(value => [value, value]));
const allEfforts = effortMap('low', 'medium', 'high', 'xhigh', 'max');
const withoutXhigh = effortMap('low', 'medium', 'high', 'max');
const throughHigh = effortMap('low', 'medium', 'high');

function modelRules(model: string): ModelRequestRules {
  let minimum = 1;
  let reasoningEfforts = allEfforts;
  if (/^claude-(opus|sonnet)-4-6$/.test(model)) { minimum = 0; reasoningEfforts = withoutXhigh; }
  else if (/^claude-mythos-preview$/.test(model)) reasoningEfforts = withoutXhigh;
  else if (/^claude-opus-4-5(-[0-9]{8})?$/.test(model)) { minimum = 0; reasoningEfforts = throughHigh; }
  else if (/^claude-(sonnet|haiku)-4-5(-[0-9]{8})?$/.test(model)) { minimum = 0; reasoningEfforts = {}; }
  return { temperature: temperatureRange(minimum, 1), reasoningEfforts, structuredOutput };
}

function object(value: unknown): JSONRecord {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new TypeError('Claude response object is invalid');
  return value as JSONRecord;
}

function parse(body: string | Uint8Array): JSONRecord {
  return object(JSON.parse(typeof body === 'string' ? body : Buffer.from(body).toString('utf8')) as unknown);
}

function string(value: unknown): string {
  if (value === undefined || value === null) return '';
  if (typeof value !== 'string') throw new TypeError('Claude response string field is invalid');
  return value;
}

function count(value: unknown): number {
  if (value === undefined || value === null) return 0;
  if (typeof value !== 'number' || !Number.isSafeInteger(value) || value < 0) throw new TypeError('Claude token count is invalid');
  return value;
}

function mergeUsage(target: JSONRecord, update: unknown): void {
  if (update === undefined || update === null) return;
  const source = object(update);
  for (const key of ['input_tokens', 'cache_creation_input_tokens', 'cache_read_input_tokens', 'output_tokens']) {
    if (source[key] !== undefined && source[key] !== null) target[key] = source[key];
  }
  if (source.output_tokens_details !== undefined && source.output_tokens_details !== null) target.output_tokens_details = source.output_tokens_details;
}

function usageFrom(source: JSONRecord): Usage {
  const cached = count(source.cache_read_input_tokens);
  const input = count(source.input_tokens) + count(source.cache_creation_input_tokens) + cached;
  const output = count(source.output_tokens);
  const details = source.output_tokens_details ? object(source.output_tokens_details) : {};
  return {
    inputTokens: input, cachedInputTokens: cached, outputTokens: output,
    reasoningTokens: count(details.thinking_tokens), totalTokens: input + output,
  };
}

function appendPart(response: DecodedResponse, text: string, isReasoning: boolean): void {
  if (!text) return;
  const previous = response.parts.at(-1);
  if (previous && !!previous.isReasoning === isReasoning) previous.text += text;
  else response.parts.push({ text, ...(isReasoning ? { isReasoning: true } : {}) } satisfies ResponsePart);
}

function decodeMessage(message: JSONRecord): DecodedResponse {
  if (message.type === 'error') throw new ProviderReportedError(readErrorTokens(Buffer.from(JSON.stringify(message)), pointers));
  if (message.type !== 'message') throw new TypeError('Claude response is not a message');
  const response: DecodedResponse = {
    parts: [], responseId: string(message.id), servedModel: string(message.model),
    providerFinishReason: string(message.stop_reason), usage: usageFrom(message.usage ? object(message.usage) : {}),
  };
  for (const item of Array.isArray(message.content) ? message.content : []) {
    const block = object(item);
    if (block.type === 'text') appendPart(response, string(block.text), false);
    if (block.type === 'thinking') appendPart(response, string(block.thinking), true);
  }
  return response;
}

/** Claude Messages dialect of the shared text generation Query. */
export function newClaudeWireFormat(workspaceID: string): WireFormat {
  const stateFor = new WeakMap<DecodedResponse, { started: boolean; blocks: Map<number, string>; usage: JSONRecord }>();
  return {
    providerName: 'anthropic', modelIDRule: 'body',
    features: {
      supportsInstructions: true, supportsStructuredOutput: true, supportsMaxOutputTokens: true,
      supportsTemperature: true, supportsReasoningEffort: true,
    },
    credentialHeader: { name: 'Authorization', prefix: 'Bearer ' },
    rulesForModel: modelRules,
    encodeRequest({ request, rules }) {
      const body: JSONRecord = {
        model: request.model, max_tokens: request.maxOutputTokens || 16_000,
        messages: request.messages.map(message => ({ role: message.role, content: message.text })),
        stream: true,
      };
      if (request.instructions) body.system = request.instructions;
      if (request.temperature !== undefined) body.temperature = request.temperature;
      const outputConfig: JSONRecord = {};
      if (request.reasoningEffort) outputConfig.effort = rules.reasoningEfforts[request.reasoningEffort];
      if (request.structuredOutput) {
        const output = request.structuredOutput;
        const schema = { ...output.schema };
        if (output.description && !schema.description) schema.description = output.description;
        outputConfig.format = { type: 'json_schema', schema };
      }
      if (Object.keys(outputConfig).length) body.output_config = outputConfig;
      return {
        path: '/v1/messages', body: Buffer.from(JSON.stringify(body)), isStreaming: true,
        headers: { 'anthropic-version': '2023-06-01', ...(workspaceID ? { 'anthropic-workspace-id': workspaceID } : {}) },
      };
    },
    decodeResponse(body) { return decodeMessage(parse(body)); },
    decodeStreamEvent(event, state, writeTextDelta) {
      const message = parse(event.data);
      const type = string(message.type);
      if (event.event === 'error' || type === 'error') throw new ProviderReportedError(readErrorTokens(Buffer.from(event.data), pointers));
      let decoder = stateFor.get(state.response);
      if (!decoder) {
        decoder = { started: false, blocks: new Map<number, string>(), usage: {} };
        stateFor.set(state.response, decoder);
      }
      if (type === 'message_start') {
        if (decoder.started || !message.message) throw new TypeError('Claude message_start is missing or repeated');
        const started = object(message.message);
        decoder.started = true;
        state.response.responseId = string(started.id);
        state.response.servedModel = string(started.model);
        mergeUsage(decoder.usage, started.usage);
        return;
      }
      if (type === 'ping' || !['content_block_start', 'content_block_delta', 'content_block_stop', 'message_delta', 'message_stop'].includes(type)) return;
      if (!decoder.started) throw new TypeError('Claude event arrived before message_start');
      if (type === 'content_block_start') {
        if (!Number.isSafeInteger(message.index) || !message.content_block) throw new TypeError('Claude content block is invalid');
        const block = object(message.content_block);
        decoder.blocks.set(message.index as number, string(block.type));
        if (block.type === 'text') { const text = string(block.text); appendPart(state.response, text, false); if (text) writeTextDelta(text); }
        if (block.type === 'thinking') appendPart(state.response, string(block.thinking), true);
      } else if (type === 'content_block_delta') {
        const delta = object(message.delta);
        const blockType = decoder.blocks.get(message.index as number);
        if (delta.type === 'text_delta') {
          if (blockType !== 'text') throw new TypeError('Claude text delta has no text block');
          const text = string(delta.text);
          appendPart(state.response, text, false);
          if (text) writeTextDelta(text);
        } else if (delta.type === 'thinking_delta') {
          if (blockType !== 'thinking') throw new TypeError('Claude thinking delta has no thinking block');
          appendPart(state.response, string(delta.thinking), true);
        }
      } else if (type === 'message_delta') {
        const delta = object(message.delta);
        if (delta.stop_reason !== undefined && delta.stop_reason !== null) state.response.providerFinishReason = string(delta.stop_reason);
        mergeUsage(decoder.usage, message.usage);
      } else if (type === 'message_stop') {
        state.response.usage = usageFrom(decoder.usage);
        state.hasFinished = true;
        state.isDone = true;
      }
    },
    finishReasons: {
      end_turn: 'stop', stop_sequence: 'stop', max_tokens: 'length',
      model_context_window_exceeded: 'length', refusal: 'refusal',
    },
    errorRules: [
      { statusCode: 0, errorToken: 'enforced_spend_limit_reached', outcome: quotaExhaustedOutcome() },
      { statusCode: 0, errorToken: 'billing_error', outcome: quotaExhaustedOutcome() },
      { statusCode: 501, outcome: providerRejectedOutcome('PROVIDER_REJECTION') },
      ...['overloaded_error', 'api_error', 'timeout_error'].map(errorToken =>
        ({ statusCode: 0, errorToken, outcome: retryOutcome('AVAILABILITY') })),
      { statusCode: 0, errorToken: 'rate_limit_error', outcome: retryOutcome('RATE_LIMIT') },
    ],
    errorTokenPointers: pointers,
    requestIDHeaders: ['request-id'],
    rateLimitHeaders: [
      'anthropic-ratelimit-requests-limit', 'anthropic-ratelimit-requests-remaining',
      'anthropic-ratelimit-tokens-limit', 'anthropic-ratelimit-tokens-remaining',
      'anthropic-ratelimit-input-tokens-limit', 'anthropic-ratelimit-input-tokens-remaining',
      'anthropic-ratelimit-output-tokens-limit', 'anthropic-ratelimit-output-tokens-remaining',
    ],
  };
}
