// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { FailureKindSchema, type FailureKind } from '../errors.js';
import type { FinishReason, ModelIDRule, ReasoningEffort, TextGenerationRequest, Usage } from './contract.js';
import { movableSchemaKeywords, removableSchemaKeywords } from './structured_output.js';

/** Optional portable fields the wire format actually sends. */
export interface RequestFeatures { /** Instructions. */ supportsInstructions?: boolean; /** Structured output. */ supportsStructuredOutput?: boolean; /** Token cap. */ supportsMaxOutputTokens?: boolean; /** Temperature. */ supportsTemperature?: boolean; /** Reasoning. */ supportsReasoningEffort?: boolean }
/** Credential header and safe public prefix. */
export interface CredentialHeader { /** HTTP header name. */ name: string; /** Public prefix before the secret. */ prefix: string }
/** Model sampling policy; accepted is the zero behavior. */
export type TemperaturePolicy = { kind: 'accepted' } | { kind: 'range'; minimum: number; maximum: number } | { kind: 'notAccepted' };
/** Accept finite nonnegative temperatures. */ export function temperatureAccepted(): TemperaturePolicy { return { kind: 'accepted' }; }
/** Accept one inclusive temperature range. */ export function temperatureRange(minimum: number, maximum: number): TemperaturePolicy { return { kind: 'range', minimum, maximum }; }
/** Reject temperature for fixed-sampling models. */ export function temperatureNotAccepted(): TemperaturePolicy { return { kind: 'notAccepted' }; }
/** How the provider receives structured output. */
export type StructuredOutputMode = 'none' | 'jsonSchema' | 'jsonObjectWithInstruction';
/** Per-model structured-output transforms; local validation retains original constraints. */
export interface StructuredOutputRules { /** Provider request mode. */ mode: StructuredOutputMode; /** Constraints moved into descriptions. */ keywordsMovedToDescription?: string[]; /** Constraints omitted on the wire. */ keywordsRemoved?: string[] }
/** Validate portable schema transform declarations. */
export function validateStructuredOutputRules(rules: StructuredOutputRules): void {
  if (!['none','jsonSchema','jsonObjectWithInstruction'].includes(rules.mode)) throw new TypeError(`structured output mode ${JSON.stringify(rules.mode)} is invalid`);
  for (const keyword of rules.keywordsMovedToDescription ?? []) if (!movableSchemaKeywords.has(keyword)) throw new TypeError(`schema keyword ${JSON.stringify(keyword)} cannot be moved to a description`);
  for (const keyword of rules.keywordsRemoved ?? []) if (!removableSchemaKeywords.has(keyword)) throw new TypeError(`schema keyword ${JSON.stringify(keyword)} cannot be removed`);
}
/** Sampling, reasoning, and structured-output rules for one model. */
export interface ModelRequestRules { /** Temperature policy. */ temperature: TemperaturePolicy; /** Accepted effort to wire value. */ reasoningEfforts: Partial<Record<ReasoningEffort,string>>; /** Schema mode and transforms. */ structuredOutput: StructuredOutputRules }
/** Validated request and selected model rules passed to an encoder. */
export interface EncodeRequestInput { /** Canonical request with transformed provider schema. */ request: TextGenerationRequest; /** Selected model rules. */ rules: ModelRequestRules }
/** One provider HTTP request without its credential. */
export interface EncodedRequest { /** Path including leading slash. */ path: string; /** JSON request bytes. */ body: Uint8Array; /** Extra safe headers. */ headers?: Record<string,string>; /** Whether event-stream is requested. */ isStreaming: boolean }
/** One decoded text or reasoning segment. */ export interface ResponsePart { /** Segment text. */ text: string; /** True for hidden reasoning. */ isReasoning?: boolean }
/** Provider response decoded to the portable shape. */
export interface DecodedResponse { /** Ordered output segments. */ parts: ResponsePart[]; /** Model echo. */ servedModel?: string; /** Response identity. */ responseId?: string; /** Provider finish token. */ providerFinishReason?: string; /** Model refusal. */ isRefusal?: boolean; /** Usage. */ usage?: Partial<Usage> }
/** A complete server-sent event. */ export interface ServerSentEvent { /** Event data lines joined by newline. */ data: string; /** Optional event type. */ event?: string }
/** Adapter for one provider's request and response dialect. Functions must be concurrency safe. */
export interface WireFormat {
  /** Provider identity in failures and receipts. */ providerName: string;
  /** Model ID validation based on its wire placement. */ modelIDRule: ModelIDRule;
  /** Optional request fields actually transmitted. */ features: RequestFeatures;
  /** Sole credential location. */ credentialHeader: CredentialHeader;
  /** Return per-model rules once after model validation. */ rulesForModel(model: string): ModelRequestRules;
  /** Encode only a validated request. */ encodeRequest(input: EncodeRequestInput): EncodedRequest;
  /** Decode a complete 2xx JSON body. */ decodeResponse?: (body: Uint8Array) => DecodedResponse;
  /** Decode one event; undefined means no completed response yet. */ decodeStreamEvent?: (event: ServerSentEvent, state: StreamDecodeState, writeTextDelta: (delta: string) => void) => void;
  /** Accepted provider token to normalized finish reason. */ finishReasons: Record<string, FinishReason>;
  /** Ordered provider-specific error rules. */ errorRules?: ErrorRule[];
  /** JSON pointers for safe error tokens. */ errorTokenPointers?: string[];
  /** Optional provider-specific retry delay reader in milliseconds. */ readErrorRetryDelay?: (body: Uint8Array) => number;
  /** Response header preference for request ID. */ requestIDHeaders?: string[];
  /** Whitelisted receipt metadata headers. */ rateLimitHeaders?: string[];
  /** No-byte timeout in milliseconds; zero disables it. */ stallTimeoutMs?: number;
}
/** Mutable state for a single event stream, owned by the pipeline. */
export interface StreamDecodeState { /** Accumulated decoded response. */ response: DecodedResponse; /** A finish token has arrived. */ hasFinished: boolean; /** Terminal DONE event arrived. */ isDone: boolean }
/** One classified provider error rule. */
export interface ErrorRule { /** Zero matches any status, including 2xx error objects. */ statusCode: number; /** Optional exact safe token. */ errorToken?: string; /** Chosen outcome. */ outcome: ErrorOutcome }
/** Terminal or retry disposition and failure kind. */
export interface ErrorOutcome { /** Classification. */ disposition: 'retry'|'providerRejected'|'invalidResponse'|'blocked'; /** Provider-neutral failure category. */ kind: FailureKind }
/** Retry a safely repeatable query. */ export function retryOutcome(kind: FailureKind): ErrorOutcome { return { disposition: 'retry', kind }; }
/** Select conclusive provider rejection. */ export function providerRejectedOutcome(kind: FailureKind): ErrorOutcome { return { disposition: 'providerRejected', kind }; }
/** Select exhausted spend or credit. */ export function quotaExhaustedOutcome(): ErrorOutcome { return providerRejectedOutcome('QUOTA_EXHAUSTED'); }
/** Select a content policy block. */ export function blockedOutcome(): ErrorOutcome { return { disposition: 'blocked', kind: 'PROVIDER_REJECTION' }; }
/** Select a provider contract violation. */ export function invalidResponseOutcome(): ErrorOutcome { return { disposition: 'invalidResponse', kind: 'PROTOCOL' }; }
/** Provider error embedded in a 2xx response or stream. Tokens alone may reach a failure. */
export class ProviderReportedError extends Error {
  /** Bounded machine-readable tokens. */ readonly errorTokens: string[];
  /** Construct without exposing provider message text. */
  constructor(errorTokens: string[]) { super('provider reported an error inside a 2xx response'); this.name = 'ProviderReportedError'; this.errorTokens = errorTokens; }
}
/** Classify non-2xx or embedded errors using first matching rule and Go defaults. */
export function classifyError(rules: readonly ErrorRule[], statusCode: number, tokens: readonly string[], isReported = false): ErrorOutcome {
  for (const rule of rules) if ((rule.statusCode === 0 || rule.statusCode === statusCode) && (!isReported || rule.statusCode === 0) && (!rule.errorToken || tokens.includes(rule.errorToken))) return rule.outcome;
  if (isReported) return invalidResponseOutcome();
  if (statusCode === 429) return retryOutcome('RATE_LIMIT');
  if (statusCode === 408 || statusCode >= 500 && statusCode !== 501) return retryOutcome('AVAILABILITY');
  const kinds: Record<number,FailureKind> = { 401:'AUTHENTICATION', 402:'QUOTA_EXHAUSTED', 403:'AUTHORIZATION', 404:'NOT_FOUND', 409:'CONFLICT' };
  if (statusCode >= 300 && statusCode < 400) return invalidResponseOutcome();
  return providerRejectedOutcome(kinds[statusCode] ?? 'PROVIDER_REJECTION');
}
/** Reject unsafe or incomplete provider declarations before any call. */
export function validateWireFormat(format: WireFormat): void {
  if (!format.providerName) throw new TypeError('wire format provider name is required');
  if (format.modelIDRule !== 'body' && format.modelIDRule !== 'pathSegment') throw new TypeError('wire format model ID rule is invalid');
  if (typeof format.rulesForModel !== 'function' || typeof format.encodeRequest !== 'function') throw new TypeError('wire format rulesForModel and encodeRequest are required');
  if (!format.decodeResponse && !format.decodeStreamEvent) throw new TypeError('wire format requires decodeResponse, decodeStreamEvent, or both');
  if (typeof format.credentialHeader?.name !== 'string' || !/^[!#$%&'*+.^_`|~0-9A-Za-z-]{1,128}$/.test(format.credentialHeader.name)) throw new TypeError('wire format credential header name is invalid');
  if (typeof format.credentialHeader.prefix !== 'string' || !/^[\x20-\x7e]*$/.test(format.credentialHeader.prefix)) throw new TypeError('wire format credential header prefix must be printable ASCII');
  for (const [token, reason] of Object.entries(format.finishReasons)) if (!/^[A-Za-z0-9_.:-]{1,64}$/.test(token) || !['stop','length','contentPolicy','refusal'].includes(reason)) throw new TypeError('wire format finish token is invalid');
  for (const [index, rule] of (format.errorRules ?? []).entries()) {
    if (!Number.isInteger(rule.statusCode) || rule.statusCode !== 0 && (rule.statusCode < 300 || rule.statusCode > 599)) throw new TypeError(`error rule ${index} status is invalid`);
    if (rule.errorToken && !/^[A-Za-z0-9_.:-]{1,64}$/.test(rule.errorToken)) throw new TypeError(`error rule ${index} token is invalid`);
    if (!rule.outcome || !['retry','providerRejected','invalidResponse','blocked'].includes(rule.outcome.disposition) || !FailureKindSchema.safeParse(rule.outcome.kind).success) throw new TypeError(`error rule ${index} outcome is invalid`);
  }
  for (const pointer of format.errorTokenPointers ?? []) if (pointer !== '' && !pointer.startsWith('/')) throw new TypeError('wire format error-token pointer must start with /');
  for (const name of [...(format.requestIDHeaders ?? []), ...(format.rateLimitHeaders ?? [])]) if (!/^[!#$%&'*+.^_`|~0-9A-Za-z-]{1,128}$/.test(name)) throw new TypeError('wire format response header name is invalid');
  if (!Number.isSafeInteger(format.stallTimeoutMs ?? 0) || (format.stallTimeoutMs ?? 0) < 0) throw new TypeError('wire format stall timeout cannot be negative');
}
