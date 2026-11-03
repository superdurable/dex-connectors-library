// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { readErrorTokens } from '../../providerhttp/index.js';
import {
  type CredentialHeader, type DecodedResponse, type ErrorRule, type ModelRequestRules,
  type ResponsePart, type StructuredOutputRules, type TemperaturePolicy, type WireFormat,
  ProviderReportedError, temperatureAccepted, validateStructuredOutputRules,
} from '../wire_format.js';
import type { FinishReason, ReasoningEffort, Usage } from '../contract.js';

/** Role used for instructions in Chat Completions. */ export type InstructionsRole = 'system'|'developer';
/** Token limit field accepted by this provider. */ export type MaxTokensField = 'max_tokens'|'max_completion_tokens';
/** Whether requests use Chat Completions event streams. */ export type StreamingPolicy = 'never'|'always';
/** First matching model rule overrides profile settings. */
export interface ModelRule {
  /** Exact leading model ID text. */ modelIDPrefix?: string;
  /** Anchored model ID regular expression. */ modelIDPattern?: string;
  /** Sampling override. */ temperature?: TemperaturePolicy;
  /** Accepted effort override; empty rejects all. */ reasoningEfforts?: Partial<Record<ReasoningEffort,string>>;
  /** Schema mode override. */ structuredOutput?: StructuredOutputRules;
  /** Token field override. */ maxTokensField?: MaxTokensField;
  /** Streaming override. */ streaming?: StreamingPolicy;
}
/** Declarative provider Chat Completions dialect. */
export interface Profile {
  /** Provider identity in failures. */ providerName: string;
  /** Endpoint path; defaults to /chat/completions. */ chatCompletionsPath?: string;
  /** Credential header; defaults to Authorization: Bearer. */ credentialHeader?: CredentialHeader;
  /** Public fixed request headers. */ fixedHeaders?: Record<string,string>;
  /** Instruction role; defaults to system. */ instructionsRole?: InstructionsRole;
  /** Token field; defaults to max_tokens. */ maxTokensField?: MaxTokensField;
  /** Accepted reasoning values. */ reasoningEfforts?: Partial<Record<ReasoningEffort,string>>;
  /** Sampling policy; defaults to accepted. */ temperature?: TemperaturePolicy;
  /** Structured output rules; defaults to none. */ structuredOutput?: StructuredOutputRules;
  /** Send strict true in JSON schema mode. */ shouldSendStrictJSONSchema?: boolean;
  /** Streaming policy; defaults to never. */ streaming?: StreamingPolicy;
  /** Request usage with stream_options. */ shouldRequestStreamUsage?: boolean;
  /** Optional allowlist of top-level body fields. */ allowedRequestFields?: string[];
  /** Provider finish-token overrides. */ finishReasons?: Record<string,FinishReason>;
  /** Error classification rules. */ errorRules?: ErrorRule[];
  /** JSON pointers of safe error tokens. */ errorTokenPointers?: string[];
  /** Response request-ID headers. */ requestIDHeaders?: string[];
  /** Receipt metadata headers. */ rateLimitHeaders?: string[];
  /** No-byte timeout in milliseconds. */ stallTimeoutMs?: number;
  /** First matching model override wins. */ modelRules?: ModelRule[];
}
interface Settings { rules: ModelRequestRules; maxTokensField: MaxTokensField; streaming: StreamingPolicy }
function requestField(value: string): boolean { return /^[a-z][a-z0-9_]{0,63}$/.test(value); }
function validateSettings(field: MaxTokensField | undefined, streaming: StreamingPolicy | undefined, efforts: Partial<Record<ReasoningEffort,string>> | undefined, structured: StructuredOutputRules | undefined): void {
  if (field && field !== 'max_tokens' && field !== 'max_completion_tokens') throw new TypeError('profile max tokens field is invalid');
  if (streaming && streaming !== 'never' && streaming !== 'always') throw new TypeError('profile streaming policy is invalid');
  for (const [effort,value] of Object.entries(efforts ?? {})) {
    if (!['none','minimal','low','medium','high','xhigh','max'].includes(effort)) throw new TypeError(`profile reasoning effort ${JSON.stringify(effort)} is unknown`);
    if (!value || !requestField(value)) throw new TypeError('profile reasoning effort wire value is invalid');
  }
  if (structured) validateStructuredOutputRules(structured);
}
/** Validate a profile and return a provider-neutral textgen wire format. */
export function newWireFormat(profile: Profile): WireFormat {
  if (!profile) throw new TypeError('openaichat profile is required');
  if (!profile.providerName?.trim()) throw new TypeError('openaichat profile provider name is required');
  if (profile.chatCompletionsPath && (!profile.chatCompletionsPath.startsWith('/') || /[?# ]/.test(profile.chatCompletionsPath))) throw new TypeError('openaichat profile chat completions path is invalid');
  if (profile.instructionsRole && !['system','developer'].includes(profile.instructionsRole)) throw new TypeError('openaichat profile instructions role is invalid');
  validateSettings(profile.maxTokensField,profile.streaming,profile.reasoningEfforts,profile.structuredOutput);
  const credentialHeader = profile.credentialHeader?.name ? profile.credentialHeader : { name:'Authorization',prefix:'Bearer ' };
  const reserved = new Set([credentialHeader.name,'Content-Type','Content-Length','Accept','Host'].map(name => name.toLowerCase()));
  for (const [name,value] of Object.entries(profile.fixedHeaders ?? {})) {
    if (!/^[!#$%&'*+.^_`|~0-9A-Za-z-]{1,128}$/.test(name) || reserved.has(name.toLowerCase()) ||
      !/^[\x20-\x7e]*$/.test(value)) throw new TypeError('openaichat profile fixed header is invalid or reserved');
  }
  for (const field of profile.allowedRequestFields ?? []) if (!requestField(field)) throw new TypeError('openaichat profile allowed request field is invalid');
  for (const [index,rule] of (profile.modelRules ?? []).entries()) {
    if (!!rule.modelIDPrefix === !!rule.modelIDPattern) throw new TypeError(`openaichat profile model rule ${index} must set exactly one model matcher`);
    if (rule.modelIDPattern && (!rule.modelIDPattern.startsWith('^') || !rule.modelIDPattern.endsWith('$'))) throw new TypeError(`openaichat profile model rule ${index} pattern must be anchored`);
    if (rule.modelIDPattern) { try { new RegExp(rule.modelIDPattern); } catch { throw new TypeError(`openaichat profile model rule ${index} pattern is invalid`); } }
    validateSettings(rule.maxTokensField,rule.streaming,rule.reasoningEfforts,rule.structuredOutput);
  }
  const allowed = profile.allowedRequestFields?.length ? new Set(profile.allowedRequestFields) : undefined;
  if (allowed) {
    const required = ['model','messages'];
    if (profile.streaming === 'always' || profile.modelRules?.some(rule => rule.streaming === 'always')) required.push('stream');
    if (required.includes('stream') && profile.shouldRequestStreamUsage) required.push('stream_options');
    for (const field of required) if (!allowed.has(field)) throw new TypeError(`openaichat profile allowed request fields must include ${JSON.stringify(field)}, which the wire format sends`);
  }
  const baseSettings: Settings = {
    rules: {
      temperature: profile.temperature ?? temperatureAccepted(),
      reasoningEfforts: profile.reasoningEfforts ?? {},
      structuredOutput: profile.structuredOutput ?? { mode:'none' },
    },
    maxTokensField: profile.maxTokensField ?? 'max_tokens',
    streaming: profile.streaming ?? 'never',
  };
  const settingsForModel = (model: string): Settings => {
    for (const rule of profile.modelRules ?? []) {
      const matches = rule.modelIDPrefix ? model.startsWith(rule.modelIDPrefix) : new RegExp(rule.modelIDPattern ?? '^$').test(model);
      if (!matches) continue;
      return {
        rules: {
          temperature: rule.temperature ?? baseSettings.rules.temperature,
          reasoningEfforts: rule.reasoningEfforts ?? baseSettings.rules.reasoningEfforts,
          structuredOutput: rule.structuredOutput ?? baseSettings.rules.structuredOutput,
        },
        maxTokensField: rule.maxTokensField ?? baseSettings.maxTokensField,
        streaming: rule.streaming ?? baseSettings.streaming,
      };
    }
    return baseSettings;
  };
  const pointers = profile.errorTokenPointers ?? ['/error/type','/error/code'];
  const reportedError = (body: Uint8Array): ProviderReportedError => new ProviderReportedError(readErrorTokens(body,pointers));
  return {
    providerName: profile.providerName, modelIDRule:'body',
    features: { supportsInstructions:true,supportsStructuredOutput:true,supportsMaxOutputTokens:true,supportsTemperature:true,supportsReasoningEffort:true },
    credentialHeader, rulesForModel: model => settingsForModel(model).rules,
    encodeRequest: ({request,rules}) => {
      const settings = settingsForModel(request.model ?? '');
      const messages: {role:string;content:string}[] = [];
      if (request.instructions) messages.push({ role:profile.instructionsRole ?? 'system',content:request.instructions });
      for (const message of request.messages) messages.push({ role:message.role,content:message.text });
      const body: Record<string,unknown> = { model:request.model,messages };
      if (request.maxOutputTokens) body[settings.maxTokensField] = request.maxOutputTokens;
      if (request.temperature != null) body.temperature = request.temperature;
      if (request.reasoningEffort) body.reasoning_effort = rules.reasoningEfforts[request.reasoningEffort];
      if (request.structuredOutput) {
        if (rules.structuredOutput.mode === 'jsonSchema') {
          const jsonSchema: Record<string,unknown> = { name:request.structuredOutput.name,schema:request.structuredOutput.schema };
          if (request.structuredOutput.description) jsonSchema.description = request.structuredOutput.description;
          if (profile.shouldSendStrictJSONSchema) jsonSchema.strict = true;
          body.response_format = { type:'json_schema',json_schema:jsonSchema };
        } else if (rules.structuredOutput.mode === 'jsonObjectWithInstruction') body.response_format = { type:'json_object' };
      }
      const isStreaming = settings.streaming === 'always';
      if (isStreaming) { body.stream = true; if (profile.shouldRequestStreamUsage) body.stream_options = { include_usage:true }; }
      if (allowed) for (const field of Object.keys(body).sort()) if (!allowed.has(field)) throw new TypeError(`the provider does not accept the ${JSON.stringify(field)} request field`);
      return { path:profile.chatCompletionsPath ?? '/chat/completions',body:Buffer.from(JSON.stringify(body)),headers:{...profile.fixedHeaders},isStreaming };
    },
    decodeResponse: body => {
      const completion = parseObject(body);
      if (completion.error !== undefined && completion.error !== null) throw reportedError(body);
      validateCompletion(completion);
      const choice = firstChoice(completion.choices);
      if (!choice) throw new TypeError('chat completion has no choice');
      const response: DecodedResponse = { parts:[],servedModel:stringOrEmpty(completion.model),responseId:stringOrEmpty(completion.id),usage:readUsage(completion.usage) };
      appendDelta(response,choice.message,undefined);
      if (typeof choice.finish_reason === 'string') response.providerFinishReason = choice.finish_reason;
      return response;
    },
    decodeStreamEvent: (event,state,writeTextDelta) => {
      if (event.data.trim() === '[DONE]') { if (!state.hasFinished) throw new TypeError('chat completion stream ended without a finish reason'); state.isDone = true; return; }
      const chunk = parseObject(Buffer.from(event.data));
      if (chunk.error !== undefined && chunk.error !== null) throw reportedError(Buffer.from(event.data));
      validateCompletion(chunk);
      state.response.responseId ||= stringOrEmpty(chunk.id);
      state.response.servedModel ||= stringOrEmpty(chunk.model);
      if (chunk.usage !== undefined && chunk.usage !== null) state.response.usage = readUsage(chunk.usage);
      const choice = firstChoice(chunk.choices);
      if (!choice) return;
      appendDelta(state.response,choice.delta,writeTextDelta);
      if (typeof choice.finish_reason === 'string' && choice.finish_reason) { state.response.providerFinishReason = choice.finish_reason; state.hasFinished = true; }
    },
    finishReasons: { stop:'stop',length:'length',content_filter:'contentPolicy',...profile.finishReasons },
    errorRules:[...(profile.errorRules ?? [])], errorTokenPointers:[...pointers],
    requestIDHeaders:[...(profile.requestIDHeaders ?? ['x-request-id'])],
    rateLimitHeaders:[...(profile.rateLimitHeaders ?? [])], stallTimeoutMs:profile.stallTimeoutMs ?? 0,
  };
}
function parseObject(body: Uint8Array): Record<string,unknown> {
  const parsed: unknown = JSON.parse(Buffer.from(body).toString());
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) throw new TypeError('completion must be an object');
  return parsed as Record<string,unknown>;
}
function stringOrEmpty(value: unknown): string { return typeof value === 'string' ? value : ''; }
function firstChoice(value: unknown): Record<string,unknown> | undefined {
  return Array.isArray(value)
    ? value.find(choice => choice && typeof choice === 'object' && (choice.index == null || choice.index === 0)) as Record<string,unknown> | undefined
    : undefined;
}
function validateCompletion(completion: Record<string, unknown>): void {
  for (const field of ['id', 'model']) if (completion[field] != null && typeof completion[field] !== 'string') throw new TypeError('chat completion identifier must be text');
  if (completion.choices != null && !Array.isArray(completion.choices)) throw new TypeError('chat completion choices must be an array');
  for (const choice of (completion.choices as unknown[] | undefined) ?? []) {
    if (choice === null) continue;
    if (typeof choice !== 'object' || Array.isArray(choice)) throw new TypeError('chat completion choice must be an object');
    const fields = choice as Record<string, unknown>;
    if (fields.index != null && (!Number.isInteger(fields.index) || !Number.isSafeInteger(fields.index))) throw new TypeError('chat completion choice index must be an integer');
    if (fields.finish_reason != null && typeof fields.finish_reason !== 'string') throw new TypeError('chat completion finish reason must be text');
    for (const name of ['message', 'delta']) if (fields[name] != null && (typeof fields[name] !== 'object' || Array.isArray(fields[name]))) throw new TypeError('chat completion delta must be an object');
  }
  if (completion.usage != null && (typeof completion.usage !== 'object' || Array.isArray(completion.usage))) throw new TypeError('chat completion usage must be an object');
  if (completion.usage != null) {
    const usage = completion.usage as Record<string, unknown>;
    for (const name of ['prompt_tokens_details', 'completion_tokens_details']) {
      if (usage[name] != null && (typeof usage[name] !== 'object' || Array.isArray(usage[name]))) throw new TypeError('chat completion usage details must be an object');
    }
  }
}
function appendDelta(response: DecodedResponse, value: unknown, writeTextDelta: ((delta:string)=>void)|undefined): void {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return;
  const delta = value as Record<string,unknown>;
  if (delta.reasoning_content !== undefined && delta.reasoning_content !== null && typeof delta.reasoning_content !== 'string') throw new TypeError('chat reasoning content must be text');
  if (delta.refusal !== undefined && delta.refusal !== null && typeof delta.refusal !== 'string') throw new TypeError('chat refusal must be text');
  if (typeof delta.reasoning_content === 'string' && delta.reasoning_content) appendPart(response,{text:delta.reasoning_content,isReasoning:true});
  if (typeof delta.refusal === 'string' && delta.refusal) response.isRefusal = true;
  const content = delta.content;
  let texts: string[] = [];
  if (typeof content === 'string') texts = [content];
  else if (Array.isArray(content)) {
    for (const part of content) {
      if (part === null) continue;
      if (!part || typeof part !== 'object' || Array.isArray(part)) throw new TypeError('chat content part must be an object');
      if (part.type != null && typeof part.type !== 'string' || part.text != null && typeof part.text !== 'string') throw new TypeError('chat content part fields must be text');
      if (part.type === 'text' && part.text) texts.push(part.text);
    }
  } else if (content !== undefined && content !== null) throw new TypeError('chat content must be text or typed parts');
  for (const text of texts) if (text) { appendPart(response,{text}); writeTextDelta?.(text); }
}
function appendPart(response: DecodedResponse, part: ResponsePart): void {
  const last = response.parts.at(-1);
  if (last && !!last.isReasoning === !!part.isReasoning) last.text += part.text;
  else response.parts.push(part);
}
function readUsage(value: unknown): Partial<Usage> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return {};
  const usage = value as Record<string,unknown>;
  const tokenCount = (item: unknown): number => {
    if (item == null) return 0;
    if (typeof item !== 'number' || !Number.isSafeInteger(item)) throw new TypeError('chat token count must be an integer');
    return item;
  };
  const prompt = usage.prompt_tokens_details as Record<string,unknown> | undefined;
  const completion = usage.completion_tokens_details as Record<string,unknown> | undefined;
  return {
    inputTokens: tokenCount(usage.prompt_tokens), outputTokens: tokenCount(usage.completion_tokens),
    totalTokens: tokenCount(usage.total_tokens), cachedInputTokens: tokenCount(prompt?.cached_tokens),
    reasoningTokens: tokenCount(completion?.reasoning_tokens),
  };
}
