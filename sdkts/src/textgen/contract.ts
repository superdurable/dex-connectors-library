// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { BranchDefinition } from '../types.js';
import type { Failure } from '../errors.js';
import { canonicalizePortableSchema } from './structured_output.js';

/** Conversation author accepted by portable text generation. */
export type MessageRole = 'user' | 'assistant';
/** One text-only conversation turn. */
export interface Message { /** Author of the turn. */ role: MessageRole; /** Nonempty text. */ text: string }
/** One portable JSON object schema, validated before dispatch. */
export interface StructuredOutput { /** Provider-visible name, 1-64 safe characters. */ name: string; /** Optional model guidance. */ description?: string; /** Portable JSON Schema object. */ schema: Record<string, unknown> }
/** Portable reasoning effort; model rules determine which values are accepted. */
export type ReasoningEffort = 'none' | 'minimal' | 'low' | 'medium' | 'high' | 'xhigh' | 'max';
/** Provider-neutral generateText input. Zero token limit uses provider default. */
export interface TextGenerationRequest {
  /** Optional model override; whitespace is trimmed. */ model?: string;
  /** System or developer instructions. */ instructions?: string;
  /** Ordered nonempty conversation. */ messages: Message[];
  /** Optional JSON object output contract. */ structuredOutput?: StructuredOutput;
  /** Maximum generated tokens, including reasoning tokens where counted. */ maxOutputTokens?: number;
  /** Sampling temperature; absence preserves the provider default. */ temperature?: number;
  /** Optional model reasoning effort. */ reasoningEffort?: ReasoningEffort;
}
/** Provider-neutral terminal finish classification. */
export type FinishReason = 'stop' | 'length' | 'contentPolicy' | 'refusal';
/** Token counts reported by the provider; absent counts are zero. */
export interface Usage { /** Prompt tokens. */ inputTokens: number; /** Cached prompt subset. */ cachedInputTokens: number; /** Generated tokens. */ outputTokens: number; /** Reasoning subset. */ reasoningTokens: number; /** Billed total. */ totalTokens: number }
/** Provider-neutral generateText result; only generated and truncated carry text. */
export interface TextGenerationResponse {
  /** Non-reasoning text. */ text: string;
  /** Validated model sent on the request. */ requestedModel: string;
  /** Provider-reported model, if any. */ servedModel: string;
  /** Provider response identifier, if any. */ responseId: string;
  /** Normalized stop reason, if known. */ finishReason: FinishReason | '';
  /** Safe provider stop token, if known. */ providerFinishReason: string;
  /** Reported usage. */ usage: Usage;
}
/** Stable operation identity shared by LLM providers. */
export const TextGenerationOperationID = 'generateText';
/** Required normal completion route. */ export const GeneratedBranchID = 'generated';
/** Partial output at the token limit. */ export const TruncatedBranchID = 'truncated';
/** Refusal or content policy stop. */ export const BlockedBranchID = 'blocked';
/** Conclusive provider rejection. */ export const ProviderRejectedBranchID = 'providerRejected';
/** Malformed or unusable response. */ export const InvalidResponseBranchID = 'invalidResponse';
/** Invalid local request or connector definition. */ export const DefectBranchID = 'defect';
/** Return fresh provider-neutral branches in manifest order. */
export function textGenerationBranchDefinitions(): BranchDefinition[] {
  return [
    { id: GeneratedBranchID, description: 'The model finished normally and returned text.' },
    { id: TruncatedBranchID, description: 'The model stopped at the output token limit and returned any partial text.', optional: true },
    { id: BlockedBranchID, description: 'The provider stopped the response for a content policy, or the model refused.', optional: true },
    { id: ProviderRejectedBranchID, description: 'The provider conclusively rejected the request.', optional: true },
    { id: InvalidResponseBranchID, description: 'The provider returned a malformed, oversized, or unusable response.', optional: true },
    { id: DefectBranchID, description: 'Local input, connection configuration, or connector definition is invalid.', optional: true },
  ];
}
/** Return a validation failure shared by all providers, without echoing input. */
export function validatePortableRequest(request: TextGenerationRequest): Failure | undefined {
  const message = checkPortableRequest(request).error;
  return message ? { kind: 'VALIDATION', provider: '', operation: '', message } : undefined;
}
/** Check portable fields in Go's stable validation order. */
export function portableRequestError(request: TextGenerationRequest): string | undefined {
  return checkPortableRequest(request).error;
}
/** Check portable fields and return one canonical schema for the pipeline. */
export function checkPortableRequest(request: TextGenerationRequest): { error?: string; schema?: Record<string, unknown> } {
  if (!Array.isArray(request.messages) || request.messages.length === 0) return { error:'at least one message is required' };
  for (const message of request.messages) {
    if (message.role !== 'user' && message.role !== 'assistant') return { error:'message role must be user or assistant' };
    if (typeof message.text !== 'string' || message.text === '') return { error:'every message requires text' };
  }
  if ((request.maxOutputTokens ?? 0) < 0) return { error:'maxOutputTokens cannot be negative' };
  if (request.structuredOutput !== undefined) {
    if (!/^[A-Za-z0-9_-]{1,64}$/.test(request.structuredOutput.name)) return { error:'structured output name must match ^[A-Za-z0-9_-]{1,64}$' };
    if (request.structuredOutput.schema === undefined || request.structuredOutput.schema === null) return { error:'structured output schema is required' };
    try { return { schema:canonicalizePortableSchema(request.structuredOutput.schema) }; }
    catch (error) { return { error:`structured output schema is not portable ${error instanceof Error ? error.message : ''}` }; }
  }
  return {};
}
/** Select request model before the configured model, trimming surrounding whitespace. */
export function resolveModel(requestModel: string, connectionModel: string): string { return requestModel.trim() || connectionModel; }
/** Model placement determines safe model ID syntax. */
export type ModelIDRule = 'body' | 'pathSegment';
/** Validate and canonicalize a model ID without including it in errors. */
export function validateModelID(rule: ModelIDRule, value: string): string {
  let model = value.trim();
  if (rule === 'body') {
    if (!/^[\x21-\x7e]{1,256}$/.test(model)) throw new TypeError('model ID must be 1 to 256 printable ASCII characters without spaces');
    return model;
  }
  if (rule === 'pathSegment') {
    if (model.startsWith('models/')) model = model.slice(7);
    if (!/^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/.test(model)) throw new TypeError('model ID must start with a letter or digit and contain at most 128 letters, digits, ".", "_", or "-"');
    return model;
  }
  throw new TypeError('model ID rule is invalid');
}
