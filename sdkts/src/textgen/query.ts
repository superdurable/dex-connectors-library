// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, type Call } from '../credentials.js';
import {
  isHeaderSafeCredential, parseRetryAfter, readErrorTokens, sendProviderRequest, validateBaseURL,
  BodyTooLargeError, ProviderTransportError, ProviderStalledError, AddressRefusal,
  type ProviderHTTPClient,
} from '../providerhttp/index.js';
import { ServerSentEventDecoder } from '../providerhttp/server_sent_events.js';
import type { Failure, FailureKind } from '../errors.js';
import { newQueryBranch, newQueryRetry, type QueryAttempt, type ReceiptDraft } from '../types.js';
import { validateQueryDefinition, type Query, type QueryDefinition } from '../operation.js';
import {
  BlockedBranchID, DefectBranchID, GeneratedBranchID, InvalidResponseBranchID,
  ProviderRejectedBranchID, resolveModel, textGenerationBranchDefinitions,
  TextGenerationOperationID, TruncatedBranchID, validateModelID, checkPortableRequest,
  type TextGenerationRequest, type TextGenerationResponse, type Usage,
} from './contract.js';
import { extractStructuredAnswerFromSchema, jsonObjectInstruction, transformSchemaForProvider } from './structured_output.js';
import {
  classifyError, ProviderReportedError, validateStructuredOutputRules, validateWireFormat,
  type DecodedResponse, type EncodedRequest, type ErrorOutcome, type ModelRequestRules,
  type StreamDecodeState, type WireFormat,
} from './wire_format.js';

const safeToken = /^[A-Za-z0-9_.:-]{1,64}$/;
const headerName = /^[!#$%&'*+.^_`|~0-9A-Za-z-]{1,128}$/;
const heartbeatIntervalMs = 5000;
class TextStreamWriteError extends Error {}
class LocalDefectError extends Error {}
/** Configuration for one generateText Query; durations are milliseconds. */
export interface TextGenerationQueryConfig {
  /** Generated connector operation definition and sync Step defaults. */ definition: QueryDefinition;
  /** Provider-specific adapter. */ wireFormat: WireFormat;
  /** Validated provider base URL. */ baseURL: string;
  /** Default model; blank requires a request model. */ connectionModel?: string;
  /** HTTP exchange deadline. */ requestTimeoutMs: number;
  /** Optional caller-owned HTTP transport, as with Go's HTTPClient. It must not replay redirects. */ httpClient?: ProviderHTTPClient;
  /** Resolve private credential once per validated attempt. */ resolveCredential: (call: Call) => string | SecretString | Promise<string | SecretString>;
  /** Maximum complete body or event stream bytes. */ maxResponseBytes: number;
  /** Maximum one SSE event bytes; required for streaming. */ maxStreamEventBytes?: number;
}
/** Shared provider-neutral text generation Query; concurrent invocations own isolated state.
 *
 * Each invocation resolves the model, validates portable fields in order, applies model rules,
 * resolves one credential, encodes the request, dispatches once, and classifies the response.
 * Validation or configuration failure in the first five stages selects defect with zero requests.
 * A silent request records a nil Dex heartbeat every five seconds, clearing a caller checkpoint.
 * Stream text can include retried, blocked, or invalid attempts; only the final Result is authoritative.
 * Sync Execute durability prevents a long local attempt from dispatching the billed call again.
 * Structured numbers are bounded to 256 literal characters and exponent magnitude 400.
 */
export class TextGenerationQuery implements Query<TextGenerationRequest, TextGenerationResponse> {
  readonly #definition: QueryDefinition;
  readonly #wireFormat: WireFormat;
  readonly #baseURL: string;
  readonly #connectionModel: string;
  readonly #requestTimeoutMs: number;
  readonly #httpClient: ProviderHTTPClient | undefined;
  readonly #resolveCredential: TextGenerationQueryConfig['resolveCredential'];
  readonly #maxResponseBytes: number;
  readonly #maxStreamEventBytes: number;
  /** Validate configuration before a provider request can be made. */
  constructor(config: TextGenerationQueryConfig) {
    if (!config) throw new TypeError('text generation query config is required');
    validateQueryDefinition(config.definition);
    if (config.definition.operation.operationId !== TextGenerationOperationID) throw new TypeError('text generation query definition must use operation ID "generateText"');
    const expected = textGenerationBranchDefinitions();
    if (config.definition.branches.length !== expected.length) throw new TypeError('generateText must declare exactly six branches');
    for (const branch of expected) if (!config.definition.branches.some(actual => actual.id === branch.id && !!actual.optional === !!branch.optional)) throw new TypeError(`generateText branch ${branch.id} has invalid optionality`);
    if (config.definition.stepDefaults.executeDurability !== 'sync') throw new TypeError('generateText must use sync Execute durability');
    if (config.definition.stepDefaults.heartbeatTimeoutMs && config.definition.stepDefaults.heartbeatTimeoutMs < 2*heartbeatIntervalMs) throw new TypeError('generateText heartbeat timeout must be zero or at least 10 seconds');
    if (config.definition.stepDefaults.executeMethodTimeoutMs && config.definition.stepDefaults.executeMethodTimeoutMs <= config.requestTimeoutMs) throw new TypeError('generateText Execute timeout must exceed the request timeout');
    validateWireFormat(config.wireFormat);
    this.#wireFormat = {
      ...config.wireFormat,
      features: { ...config.wireFormat.features },
      credentialHeader: { ...config.wireFormat.credentialHeader },
      finishReasons: { ...config.wireFormat.finishReasons },
      errorRules: [...(config.wireFormat.errorRules ?? [])],
      errorTokenPointers: [...(config.wireFormat.errorTokenPointers ?? [])],
      requestIDHeaders: [...(config.wireFormat.requestIDHeaders ?? [])],
      rateLimitHeaders: [...(config.wireFormat.rateLimitHeaders ?? [])],
    };
    this.#baseURL = validateBaseURL(config.baseURL);
    this.#connectionModel = (config.connectionModel ?? '').trim();
    if (this.#connectionModel) validateModelID(this.#wireFormat.modelIDRule, this.#connectionModel);
    if (!Number.isSafeInteger(config.requestTimeoutMs) || config.requestTimeoutMs <= 0) throw new TypeError('text generation query request timeout must be positive');
    if (!config.resolveCredential) throw new TypeError('text generation query credential resolver is required');
    if (!Number.isSafeInteger(config.maxResponseBytes) || config.maxResponseBytes < 1) throw new TypeError('text generation query maxResponseBytes must be positive');
    if (this.#wireFormat.decodeStreamEvent &&
      (!Number.isSafeInteger(config.maxStreamEventBytes) || (config.maxStreamEventBytes ?? 0) < 1)) {
      throw new TypeError('text generation query maxStreamEventBytes must be positive for a streaming wire format');
    }
    this.#definition = config.definition;
    this.#requestTimeoutMs = config.requestTimeoutMs;
    this.#httpClient = config.httpClient;
    this.#resolveCredential = config.resolveCredential;
    this.#maxResponseBytes = config.maxResponseBytes;
    this.#maxStreamEventBytes = config.maxStreamEventBytes ?? 0;
  }
  /** Return the connector's generateText definition. */ definition(): QueryDefinition { return this.#definition; }
  /** Return optional request features declared by the provider. */ requestFeatures(): Readonly<WireFormat['features']> { return this.#wireFormat.features; }
  /** Return the provider model ID rule. */ modelIDRule(): WireFormat['modelIDRule'] { return this.#wireFormat.modelIDRule; }
  /** Validate in fixed order, dispatch once, and classify a branch or safe retry without exposing secrets. */
  async invoke(call: Call, request: TextGenerationRequest): Promise<QueryAttempt<TextGenerationResponse>> {
    const response = emptyResponse();
    const format = this.#wireFormat;
    const attempt = new TextGenerationAttemptResult(format, response);
    if (!call?.context) return attempt.branch(DefectBranchID, 'LOCAL_DEFECT', 'a Dex Step context is required');
    let schema: Record<string, unknown> | undefined;
    let rules: ModelRequestRules;
    let encoded: EncodedRequest;
    let credential: string;
    try {
      const model = resolveModel(request.model ?? '', this.#connectionModel);
      if (!model) throw new TypeError('no model is selected; set the request Model or the connection model');
      response.requestedModel = validateModelID(format.modelIDRule, model);
      const portable = checkPortableRequest(request);
      if (portable.error) throw new TypeError(portable.error);
      schema = portable.schema;
      validateFeatures(request, format.features);
      rules = format.rulesForModel(response.requestedModel);
      validateSampling(request, rules);
      const prepared: TextGenerationRequest = { ...request, model: response.requestedModel };
      if (request.structuredOutput && schema) {
        try { validateStructuredOutputRules(rules.structuredOutput); }
        catch (error) { throw new LocalDefectError(`the wire format's structured output rules are invalid: ${error instanceof Error ? error.message : ''}`); }
        if (rules.structuredOutput.mode === 'none') throw new TypeError('the model does not support structured output');
        if (rules.structuredOutput.mode === 'jsonObjectWithInstruction' && !format.features.supportsInstructions) throw new LocalDefectError('the JSON object mode requires the Instructions feature');
        prepared.structuredOutput = { ...request.structuredOutput, schema: transformSchemaForProvider(schema, rules.structuredOutput) };
        if (rules.structuredOutput.mode === 'jsonObjectWithInstruction') {
          const instruction = jsonObjectInstruction(request.structuredOutput.name, request.structuredOutput.description ?? '', schema);
          prepared.instructions = `${request.instructions ? `${request.instructions}\n\n` : ''}${instruction}`;
        }
      }
      try {
        const resolved = await this.#resolveCredential(call);
        credential = resolved instanceof SecretString ? resolved.reveal() : resolved;
      } catch { return attempt.branch(DefectBranchID, 'AUTHENTICATION', 'connection credentials are unavailable'); }
      if (!isHeaderSafeCredential(credential)) return attempt.branch(DefectBranchID, 'AUTHENTICATION', 'connection credentials are invalid');
      try { encoded = format.encodeRequest({ request: prepared, rules }); }
      catch (error) { return attempt.branch(DefectBranchID, 'VALIDATION', `the request cannot be encoded for the provider: ${error instanceof Error ? error.message : 'invalid request'}`); }
      try { validateEncodedRequest(encoded, format.credentialHeader.name); }
      catch (error) { throw new LocalDefectError(`the wire format built an invalid request: ${error instanceof Error ? error.message : ''}`); }
      if (encoded.isStreaming && !format.decodeStreamEvent || !encoded.isStreaming && !format.decodeResponse) return attempt.branch(DefectBranchID, 'LOCAL_DEFECT', 'the wire format cannot decode the response form it requested');
    } catch (error) { return attempt.branch(DefectBranchID, error instanceof LocalDefectError ? 'LOCAL_DEFECT' : 'VALIDATION', error instanceof Error ? error.message : 'invalid request'); }
    const headers: Record<string,string> = {
      ...encoded.headers,
      'Content-Type': 'application/json',
      Accept: encoded.isStreaming ? 'text/event-stream' : 'application/json',
      [format.credentialHeader.name]: format.credentialHeader.prefix + credential,
    };
    const stream = new TextGenerationStreamAttempt(call, format, encoded.isStreaming, this.#maxStreamEventBytes, this.#maxResponseBytes);
    let hasCompleteResponse = false;
    const heartbeats = new TextGenerationHeartbeat(call);
    heartbeats.start();
    try {
      const providerRequest = {
        url: this.#baseURL + encoded.path, method: 'POST', headers, body: encoded.body,
        maxResponseBytes: this.#maxResponseBytes, maxErrorResponseBytes: 64 << 10,
        ignoreErrorBodyReadFailure: true, addressPolicy: 'unrestricted' as const,
        timeoutMs: this.#requestTimeoutMs, signal: call.context.cancellationSignal,
        ...(format.stallTimeoutMs ? { stallTimeoutMs: format.stallTimeoutMs } : {}),
        onResponseChunk: stream.onChunk.bind(stream),
      };
      const providerResponse = await (this.#httpClient ? this.#httpClient.send(providerRequest) : sendProviderRequest(providerRequest));
      hasCompleteResponse = true;
      const receipt = makeReceipt(call, format, providerResponse.headers, credential);
      const status = providerResponse.status;
      stream.wasEventStream = encoded.isStreaming && isEventStream(providerResponse.headers);
      if (status < 200 || status >= 300) {
        const tokens = safeTokens(readErrorTokens(providerResponse.body.slice(0, 64 << 10), format.errorTokenPointers ?? []), credential);
        const outcome = classifyError(format.errorRules ?? [], status, tokens);
        let delay = outcome.disposition === 'retry' ? Math.max(0, format.readErrorRetryDelay?.(providerResponse.body) ?? 0) : 0;
        if (!delay && outcome.disposition === 'retry') delay = parseRetryAfter(String(providerResponse.headers['retry-after'] ?? ''));
        return classifyOutcome(outcome, `provider returned HTTP ${status}${tokens.length ? ` ${tokens.join(' ')}` : ''}`, delay, receipt, attempt);
      }
      let decoded: DecodedResponse;
      try {
        if (stream.wasEventStream) {
          if (stream.error) throw stream.error;
          if (!stream.state.isDone && !stream.state.hasFinished) return attempt.retry('TRANSPORT','provider response ended before it was complete');
          decoded = stream.state.response;
        } else {
          if (!format.decodeResponse) return attempt.branch(InvalidResponseBranchID,'PROTOCOL','provider did not return the event stream it was asked for',receipt);
          decoded = format.decodeResponse(providerResponse.body);
        }
      } catch (error) {
        if (error instanceof ProviderReportedError) {
          const tokens = safeTokens(error.errorTokens, credential);
          return classifyOutcome(classifyError(format.errorRules ?? [], 0, tokens, true), `provider reported an error inside a 2xx response${tokens.length ? ` ${tokens.join(' ')}` : ''}`, 0, receipt, attempt);
        }
        if (error instanceof BodyTooLargeError) return attempt.branch(InvalidResponseBranchID,'RESPONSE_TOO_LARGE','provider response exceeds the configured size limit',receipt);
        if (error instanceof TextStreamWriteError) return attempt.retry('AVAILABILITY','the text stream could not be written');
        return attempt.branch(InvalidResponseBranchID,'PROTOCOL','provider response does not match its documented format',receipt);
      }
      if (!isDecodedResponse(decoded)) return attempt.branch(InvalidResponseBranchID,'PROTOCOL','provider response does not match its documented format',receipt);
      if (!boundedIdentifier(decoded.servedModel ?? '') || !boundedIdentifier(decoded.responseId ?? '') ||
        revealsCredential(decoded.servedModel ?? '',credential) || revealsCredential(decoded.responseId ?? '',credential)) {
        return attempt.branch(InvalidResponseBranchID,'PROTOCOL','provider returned an invalid model or response identifier',receipt);
      }
      response.servedModel = decoded.servedModel ?? '';
      response.responseId = decoded.responseId ?? '';
      if (response.responseId) receipt.providerObjectId = response.responseId;
      const usage = completeUsage(decoded.usage ?? {});
      if (!usage) return attempt.branch(InvalidResponseBranchID,'PROTOCOL','provider reported negative token usage',receipt);
      response.usage = usage;
      const token = decoded.providerFinishReason ?? '';
      if (revealsCredential(token,credential)) return attempt.branch(InvalidResponseBranchID,'PROTOCOL','provider returned an invalid finish reason',receipt);
      if (safeToken.test(token)) response.providerFinishReason = token;
      if (decoded.isRefusal) response.finishReason = 'refusal';
      else if (!response.providerFinishReason) return attempt.branch(InvalidResponseBranchID,'PROTOCOL','provider returned no recognizable finish reason',receipt);
      else {
        const reason = Object.hasOwn(format.finishReasons, response.providerFinishReason) ? format.finishReasons[response.providerFinishReason] : undefined;
        if (!reason) return attempt.branch(InvalidResponseBranchID,'PROTOCOL',`provider stopped for an unsupported reason: ${response.providerFinishReason}`,receipt);
        response.finishReason = reason;
      }
      const text = decoded.parts.filter(part => !part.isReasoning).map(part => part.text).join('');
      if (response.finishReason === 'stop') {
        if (!text) return attempt.branch(InvalidResponseBranchID,'PROTOCOL','provider finished without text',receipt);
        try { response.text = schema ? extractStructuredAnswerFromSchema(schema,text) : text; }
        catch (error) { return attempt.branch(InvalidResponseBranchID,'PROTOCOL',`structured output does not match its schema ${error instanceof Error ? error.message : ''}`,receipt); }
        if (!stream.wasEventStream && call.hasTextStream()) { try { call.writeText(response.text); } catch { return attempt.retry('AVAILABILITY','the text stream could not be written'); } }
        return newQueryBranch(GeneratedBranchID,response,receipt);
      }
      if (response.finishReason === 'length') {
        response.text = text;
        if (!stream.wasEventStream && text && call.hasTextStream()) { try { call.writeText(text); } catch { return attempt.retry('AVAILABILITY','the text stream could not be written'); } }
        return newQueryBranch(TruncatedBranchID,response,receipt,attempt.failure('RESPONSE_TOO_LARGE','provider stopped at the output token limit'));
      }
      return attempt.branch(BlockedBranchID,'PROVIDER_REJECTION',response.finishReason === 'refusal' ? 'the model refused the request' : 'provider stopped the response for a content policy',receipt);
    } catch (error) {
      if (stream.error !== undefined) {
        const receipt = stream.responseHeaders ? makeReceipt(call,format,stream.responseHeaders,credential) : undefined;
        if (stream.error instanceof BodyTooLargeError) return attempt.branch(InvalidResponseBranchID,'RESPONSE_TOO_LARGE','provider response exceeds the configured size limit',receipt);
        if (stream.error instanceof TextStreamWriteError) return attempt.retry('AVAILABILITY','the text stream could not be written');
        if (stream.error instanceof ProviderReportedError) {
          const tokens = safeTokens(stream.error.errorTokens, credential);
          return classifyOutcome(
            classifyError(format.errorRules ?? [], 0, tokens, true),
            `provider reported an error inside a 2xx response${tokens.length ? ` ${tokens.join(' ')}` : ''}`,
            0, receipt ?? {provider:format.providerName}, attempt,
          );
        }
        return attempt.branch(InvalidResponseBranchID,'PROTOCOL','provider response does not match its documented format',receipt);
      }
      if (error instanceof BodyTooLargeError) {
        const receipt = error.headers ? makeReceipt(call,format,error.headers,credential) : undefined;
        return attempt.branch(InvalidResponseBranchID,'RESPONSE_TOO_LARGE','provider response exceeds the configured size limit',receipt);
      }
      if (error instanceof AddressRefusal) return attempt.branch(DefectBranchID,'LOCAL_DEFECT','provider address is not allowed');
      if (error instanceof ProviderStalledError) return attempt.retry('AVAILABILITY','provider sent no bytes within the stall timeout');
      if (error instanceof ProviderTransportError) return attempt.retry('TRANSPORT',error.isProvablyNotSent ? 'provider request failed before a response was received' : 'provider response ended before it was complete');
      return hasCompleteResponse ? attempt.branch(InvalidResponseBranchID,'PROTOCOL','provider response does not match its documented format') : attempt.retry('TRANSPORT','provider response ended before it was complete');
    } finally { await heartbeats.stop(); }
  }
}
class TextGenerationAttemptResult {
  constructor(readonly format: WireFormat, readonly response: TextGenerationResponse) {}

  failure(kind: FailureKind, message: string): Failure {
    return { kind, provider: this.format.providerName, operation: TextGenerationOperationID, message };
  }

  branch(id: string, kind: FailureKind, message: string, receipt: ReceiptDraft = { provider: this.format.providerName }): QueryAttempt<TextGenerationResponse> {
    return newQueryBranch(id, this.response, receipt, this.failure(kind, message));
  }

  retry(kind: FailureKind, message: string, delay = 0): QueryAttempt<TextGenerationResponse> {
    return newQueryRetry(this.failure(kind, message), delay);
  }
}

class TextGenerationStreamAttempt {
  readonly state: StreamDecodeState = { response: { parts: [] }, hasFinished: false, isDone: false };
  readonly #decoder: ServerSentEventDecoder;
  error: unknown;
  wasEventStream = false;
  responseHeaders: Record<string, unknown> | undefined;

  constructor(readonly call: Call, readonly format: WireFormat, readonly isStreaming: boolean, maxEventBytes: number, maxTotalBytes: number) {
    this.#decoder = new ServerSentEventDecoder(maxEventBytes, maxTotalBytes);
  }

  onChunk(chunk: Uint8Array, status: number, headers: Record<string, unknown>): boolean | void {
    if (status < 200 || status >= 300 || !this.isStreaming || !isEventStream(headers)) return;
    this.wasEventStream = true;
    this.responseHeaders = headers;
    try { return this.#decoder.feed(chunk, this.decodeEvent.bind(this)); }
    catch (error) { this.error = error; throw error; }
  }

  private decodeEvent(event: { data: string; event?: string }): boolean {
    this.format.decodeStreamEvent?.(event, this.state, this.writeTextDelta.bind(this));
    return !this.state.isDone;
  }

  private writeTextDelta(delta: string): void {
    if (!delta || !this.call.hasTextStream()) return;
    try { this.call.writeText(delta); } catch { throw new TextStreamWriteError(); }
  }
}

class TextGenerationHeartbeat {
  #inFlight = Promise.resolve();
  #timer: ReturnType<typeof setInterval> | undefined;

  constructor(readonly call: Call) {}

  start(): void { this.#timer = setInterval(this.recordHeartbeat.bind(this), heartbeatIntervalMs); }

  async stop(): Promise<void> {
    if (this.#timer) clearInterval(this.#timer);
    await this.#inFlight;
  }

  private recordHeartbeat(): void {
    this.#inFlight = this.#inFlight.then(async () => {
      try { await this.call.context.recordHeartbeat(undefined); }
      catch { /* Dex owns failed-heartbeat timeout handling. */ }
    });
  }
}

function isEventStream(headers: Record<string, unknown>): boolean {
  return /^text\/event-stream(?:\s*;|$)/i.test(String(headers['content-type'] ?? '').trim());
}
/** Construct a generateText Query, rejecting invalid definitions, wire formats, models, timeouts, and size limits before dispatch. */
export function newTextGenerationQuery(config: TextGenerationQueryConfig): TextGenerationQuery { return new TextGenerationQuery(config); }
function emptyResponse(): TextGenerationResponse {
  return {
    text: '', requestedModel: '', servedModel: '', responseId: '', finishReason: '', providerFinishReason: '',
    usage: { inputTokens: 0, cachedInputTokens: 0, outputTokens: 0, reasoningTokens: 0, totalTokens: 0 },
  };
}
function isDecodedResponse(value: unknown): value is DecodedResponse {
  if (value === null || typeof value !== 'object') return false;
  const response = value as Record<string, unknown>;
  if (!Array.isArray(response.parts) || !response.parts.every(part => part !== null && typeof part === 'object' && typeof part.text === 'string' && (part.isReasoning === undefined || typeof part.isReasoning === 'boolean'))) return false;
  if (['servedModel', 'responseId', 'providerFinishReason'].some(name => response[name] !== undefined && typeof response[name] !== 'string')) return false;
  return (response.isRefusal === undefined || typeof response.isRefusal === 'boolean') && (response.usage === undefined || response.usage !== null && typeof response.usage === 'object' && !Array.isArray(response.usage));
}
function validateFeatures(request: TextGenerationRequest, features: WireFormat['features']): void {
  const fields: [boolean,boolean,string][] = [
    [!!request.instructions,!!features.supportsInstructions,'Instructions'],
    [!!request.structuredOutput,!!features.supportsStructuredOutput,'StructuredOutput'],
    [!!request.maxOutputTokens,!!features.supportsMaxOutputTokens,'MaxOutputTokens'],
    [request.temperature != null,!!features.supportsTemperature,'Temperature'],
    [!!request.reasoningEffort,!!features.supportsReasoningEffort,'ReasoningEffort'],
  ];
  for (const [used,supported,name] of fields) if (used && !supported) throw new TypeError(`this connector's wire format does not send ${name}; leave it unset`);
}
function validateSampling(request: TextGenerationRequest, rules: ModelRequestRules): void {
  if (request.temperature != null) {
    const value = request.temperature;
    if (!Number.isFinite(value) || value < 0) throw new TypeError('temperature must be a finite, non-negative number');
    if (rules.temperature.kind === 'notAccepted') throw new TypeError('the model does not accept a temperature; leave Temperature nil');
    if (rules.temperature.kind === 'range') {
      if (!Number.isFinite(rules.temperature.minimum) || !Number.isFinite(rules.temperature.maximum) || rules.temperature.minimum > rules.temperature.maximum) throw new TypeError("the wire format's temperature range is invalid");
      if (value < rules.temperature.minimum || value > rules.temperature.maximum) throw new TypeError(`the model accepts a temperature from ${rules.temperature.minimum} through ${rules.temperature.maximum}`);
    }
  }
  if (request.reasoningEffort) {
    if (!['none','minimal','low','medium','high','xhigh','max'].includes(request.reasoningEffort)) throw new TypeError('reasoning effort must be none, minimal, low, medium, high, xhigh, or max');
    if (!(request.reasoningEffort in rules.reasoningEfforts)) throw new TypeError(`the model does not accept reasoning effort ${JSON.stringify(request.reasoningEffort)}`);
  }
}
function validateEncodedRequest(encoded: EncodedRequest, credentialHeaderName: string): void {
  if (!encoded.path.startsWith('/')) throw new TypeError('encoded request path must start with /');
  if (!/^[\x21-\x7e]+$/.test(encoded.path) || encoded.path.includes('#')) throw new TypeError('encoded request path must be printable ASCII without spaces or fragments');
  const reserved = new Set([credentialHeaderName,'content-type','content-length','accept','host'].map(value => value.toLowerCase()));
  for (const [name,value] of Object.entries(encoded.headers ?? {})) if (!headerName.test(name) || reserved.has(name.toLowerCase()) || !/^[\x20-\x7e]*$/.test(value)) throw new TypeError('encoded request header name or value is invalid');
}
function safeTokens(tokens: readonly string[], credential: string): string[] { return tokens.filter(token => safeToken.test(token) && !revealsCredential(token,credential)); }
function revealsCredential(value: string, credential: string): boolean { return !!credential && value.includes(credential); }
function boundedIdentifier(value: string): boolean { return value === '' || Buffer.byteLength(value) <= 256 && isHeaderSafeCredential(value); }
function completeUsage(partial: Partial<Usage>): Usage | undefined {
  const usage = { inputTokens: partial.inputTokens ?? 0,cachedInputTokens:partial.cachedInputTokens ?? 0,outputTokens:partial.outputTokens ?? 0,reasoningTokens:partial.reasoningTokens ?? 0,totalTokens:partial.totalTokens ?? 0 };
  if (Object.values(usage).some(value => !Number.isFinite(value) || value < 0)) return undefined;
  if (usage.totalTokens === 0) usage.totalTokens = usage.inputTokens + usage.outputTokens;
  return usage;
}
function makeReceipt(call: Call, format: WireFormat, headers: Record<string, unknown>, credential: string): ReceiptDraft {
  const receipt: ReceiptDraft = { callId: call.id, provider: format.providerName, observedAt: new Date().toISOString() };
  for (const name of format.requestIDHeaders ?? []) {
    const value = String(headers[name.toLowerCase()] ?? '').trim();
    if (boundedIdentifier(value) && value && !revealsCredential(value,credential)) {
      receipt.providerRequestId = value;
      break;
    }
  }
  for (const name of format.rateLimitHeaders ?? []) {
    const value = String(headers[name.toLowerCase()] ?? '').trim();
    if (value && value.length <= 256 && /^[\x20-\x7e]*$/.test(value) && !revealsCredential(value,credential)) {
      (receipt.metadata ??= {})[name.toLowerCase()] = value;
    }
  }
  return receipt;
}
function classifyOutcome(outcome: ErrorOutcome, message: string, delay: number, receipt: ReceiptDraft, attempt: TextGenerationAttemptResult): QueryAttempt<TextGenerationResponse> {
  if (outcome.disposition === 'retry') return attempt.retry(outcome.kind,message,Number.isFinite(delay) ? Math.floor(Math.min(3600000,Math.max(0,delay))) : 0);
  if (outcome.disposition === 'blocked') { attempt.response.finishReason = 'contentPolicy'; return attempt.branch(BlockedBranchID,outcome.kind,message,receipt); }
  return attempt.branch(outcome.disposition === 'invalidResponse' ? InvalidResponseBranchID : ProviderRejectedBranchID,outcome.kind,message,receipt);
}
