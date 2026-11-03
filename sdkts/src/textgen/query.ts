// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, type Call } from '../credentials.js';
import { isHeaderSafeCredential, parseRetryAfter, readErrorTokens, sendProviderRequest, validateBaseURL, BodyTooLargeError, ProviderTransportError, ProviderStalledError, AddressRefusal } from '../providerhttp/index.js';
import type { Failure, FailureKind } from '../errors.js';
import { newQueryBranch, newQueryRetry, type QueryAttempt, type ReceiptDraft } from '../types.js';
import { validateQueryDefinition, type Query, type QueryDefinition } from '../operation.js';
import { BlockedBranchID, DefectBranchID, GeneratedBranchID, InvalidResponseBranchID, ProviderRejectedBranchID, resolveModel, textGenerationBranchDefinitions, TextGenerationOperationID, TruncatedBranchID, validateModelID, checkPortableRequest, type TextGenerationRequest, type TextGenerationResponse, type Usage } from './contract.js';
import { extractStructuredAnswerFromSchema, jsonObjectInstruction, transformSchemaForProvider } from './structured_output.js';
import { classifyError, ProviderReportedError, validateStructuredOutputRules, validateWireFormat, type DecodedResponse, type EncodedRequest, type ErrorOutcome, type ModelRequestRules, type StreamDecodeState, type WireFormat } from './wire_format.js';

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
  /** Resolve private credential once per validated attempt. */ resolveCredential: (call: Call) => string | SecretString | Promise<string | SecretString>;
  /** Maximum complete body or event stream bytes. */ maxResponseBytes: number;
  /** Maximum one SSE event bytes; required for streaming. */ maxStreamEventBytes?: number;
}
/** Shared provider-neutral text generation Query; each invoke owns isolated state. */
export class TextGenerationQuery implements Query<TextGenerationRequest, TextGenerationResponse> {
  readonly #definition: QueryDefinition;
  readonly #wireFormat: WireFormat;
  readonly #baseURL: string;
  readonly #connectionModel: string;
  readonly #requestTimeoutMs: number;
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
    this.#wireFormat = { ...config.wireFormat, features: { ...config.wireFormat.features }, credentialHeader: { ...config.wireFormat.credentialHeader }, finishReasons: { ...config.wireFormat.finishReasons }, errorRules: [...(config.wireFormat.errorRules ?? [])], errorTokenPointers: [...(config.wireFormat.errorTokenPointers ?? [])], requestIDHeaders: [...(config.wireFormat.requestIDHeaders ?? [])], rateLimitHeaders: [...(config.wireFormat.rateLimitHeaders ?? [])] };
    this.#baseURL = validateBaseURL(config.baseURL);
    this.#connectionModel = (config.connectionModel ?? '').trim();
    if (this.#connectionModel) validateModelID(this.#wireFormat.modelIDRule, this.#connectionModel);
    if (!Number.isSafeInteger(config.requestTimeoutMs) || config.requestTimeoutMs <= 0) throw new TypeError('text generation query request timeout must be positive');
    if (!config.resolveCredential) throw new TypeError('text generation query credential resolver is required');
    if (!Number.isSafeInteger(config.maxResponseBytes) || config.maxResponseBytes < 1) throw new TypeError('text generation query maxResponseBytes must be positive');
    if (this.#wireFormat.decodeStreamEvent && (!Number.isSafeInteger(config.maxStreamEventBytes) || (config.maxStreamEventBytes ?? 0) < 1)) throw new TypeError('text generation query maxStreamEventBytes must be positive for a streaming wire format');
    this.#definition = config.definition;
    this.#requestTimeoutMs = config.requestTimeoutMs;
    this.#resolveCredential = config.resolveCredential;
    this.#maxResponseBytes = config.maxResponseBytes;
    this.#maxStreamEventBytes = config.maxStreamEventBytes ?? 0;
  }
  /** Return the connector's generateText definition. */ definition(): QueryDefinition { return this.#definition; }
  /** Return optional request features declared by the provider. */ requestFeatures(): Readonly<WireFormat['features']> { return this.#wireFormat.features; }
  /** Return the provider model ID rule. */ modelIDRule(): WireFormat['modelIDRule'] { return this.#wireFormat.modelIDRule; }
  /** Validate, dispatch once, then classify a branch or safe retry. */
  async invoke(call: Call, request: TextGenerationRequest): Promise<QueryAttempt<TextGenerationResponse>> {
    const response = emptyResponse();
    const format = this.#wireFormat;
    const failure = (kind: FailureKind, message: string): Failure => ({ kind, provider: format.providerName, operation: TextGenerationOperationID, message });
    const branch = (id: string, kind: FailureKind, message: string, receipt: ReceiptDraft = { provider: format.providerName }): QueryAttempt<TextGenerationResponse> => newQueryBranch(id, response, receipt, failure(kind, message));
    const retry = (kind: FailureKind, message: string, delay = 0): QueryAttempt<TextGenerationResponse> => newQueryRetry(failure(kind, message), delay);
    if (!call?.context) return branch(DefectBranchID, 'LOCAL_DEFECT', 'a Dex Step context is required');
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
        if (rules.structuredOutput.mode === 'jsonObjectWithInstruction') prepared.instructions = `${request.instructions ? `${request.instructions}\n\n` : ''}${jsonObjectInstruction(request.structuredOutput.name, request.structuredOutput.description ?? '', schema)}`;
      }
      try { const resolved = await this.#resolveCredential(call); credential = resolved instanceof SecretString ? resolved.reveal() : resolved; } catch { return branch(DefectBranchID, 'AUTHENTICATION', 'connection credentials are unavailable'); }
      if (!isHeaderSafeCredential(credential)) return branch(DefectBranchID, 'AUTHENTICATION', 'connection credentials are invalid');
      try { encoded = format.encodeRequest({ request: prepared, rules }); }
      catch (error) { return branch(DefectBranchID, 'VALIDATION', `the request cannot be encoded for the provider: ${error instanceof Error ? error.message : 'invalid request'}`); }
      try { validateEncodedRequest(encoded, format.credentialHeader.name); }
      catch (error) { throw new LocalDefectError(`the wire format built an invalid request: ${error instanceof Error ? error.message : ''}`); }
      if (encoded.isStreaming && !format.decodeStreamEvent || !encoded.isStreaming && !format.decodeResponse) return branch(DefectBranchID, 'LOCAL_DEFECT', 'the wire format cannot decode the response form it requested');
    } catch (error) { return branch(DefectBranchID, error instanceof LocalDefectError ? 'LOCAL_DEFECT' : 'VALIDATION', error instanceof Error ? error.message : 'invalid request'); }
    const headers: Record<string,string> = { ...encoded.headers, 'Content-Type':'application/json', Accept: encoded.isStreaming ? 'text/event-stream' : 'application/json', [format.credentialHeader.name]: format.credentialHeader.prefix + credential };
    const isLocal = new URL(this.#baseURL).protocol === 'http:';
    const streamState: StreamDecodeState = { response: { parts: [] }, hasFinished: false, isDone: false };
    let streamBuffer = '';
    let streamError: unknown;
    const textDecoder = new TextDecoder();
    let wasEventStream = false;
    const onChunk = (chunk: Uint8Array, status: number, responseHeaders: Record<string, unknown>) => {
      if (status < 200 || status >= 300 || !encoded.isStreaming || !/^text\/event-stream(?:\s*;|$)/i.test(String(responseHeaders['content-type'] ?? '').trim()) || streamError) return;
      wasEventStream = true;
      streamBuffer += textDecoder.decode(chunk,{stream:true});
      let boundary: RegExpExecArray | null;
      while ((boundary = /\r?\n\r?\n/.exec(streamBuffer)) !== null) {
        const eventText = streamBuffer.slice(0,boundary.index).replaceAll('\r','');
        streamBuffer = streamBuffer.slice(boundary.index+boundary[0].length);
        if (Buffer.byteLength(eventText) > this.#maxStreamEventBytes) { streamError = new BodyTooLargeError(); return; }
        const data = eventText.split('\n').filter(line => line.startsWith('data:')).map(line => line.slice(5).trimStart()).join('\n');
        if (!data) continue;
        try { format.decodeStreamEvent?.({ data }, streamState, delta => { if (delta && call.hasTextStream()) { try { call.writeText(delta); } catch { throw new TextStreamWriteError(); } } }); }
        catch (error) { streamError = error; return; }
      }
      if (Buffer.byteLength(streamBuffer) > this.#maxStreamEventBytes) streamError = new BodyTooLargeError();
    };
    let heartbeat = Promise.resolve();
    const heartbeatTimer = setInterval(() => { heartbeat = heartbeat.then(async () => { try { await call.context.recordHeartbeat(undefined); } catch { /* A failed heartbeat leaves timeout handling to Dex. */ } }); }, heartbeatIntervalMs);
    try {
      const providerResponse = await sendProviderRequest({ url: this.#baseURL + encoded.path, method: 'POST', headers, body: encoded.body, maxResponseBytes: this.#maxResponseBytes, maxErrorResponseBytes: 64 << 10, addressPolicy: isLocal ? 'privateNetworkHTTP' : 'publicHTTPS', timeoutMs: this.#requestTimeoutMs, signal: call.context.cancellationSignal, ...(format.stallTimeoutMs ? { stallTimeoutMs: format.stallTimeoutMs } : {}), onResponseChunk: onChunk });
      const receipt = makeReceipt(call, format, providerResponse.headers, credential);
      const status = providerResponse.status;
      if (status < 200 || status >= 300) {
        const tokens = safeTokens(readErrorTokens(providerResponse.body.slice(0, 64 << 10), format.errorTokenPointers ?? []), credential);
        const outcome = classifyError(format.errorRules ?? [], status, tokens);
        let delay = outcome.disposition === 'retry' ? Math.max(0, format.readErrorRetryDelay?.(providerResponse.body) ?? 0) : 0;
        if (!delay && outcome.disposition === 'retry') delay = parseRetryAfter(String(providerResponse.headers['retry-after'] ?? ''));
        return classifyOutcome(outcome, `provider returned HTTP ${status}${tokens.length ? ` ${tokens.join(' ')}` : ''}`, delay, receipt, response, branch, retry);
      }
      let decoded: DecodedResponse;
      try {
        if (wasEventStream) {
          if (streamError) throw streamError;
          if (!streamState.isDone && !streamState.hasFinished) return retry('TRANSPORT','provider response ended before it was complete');
          decoded = streamState.response;
        } else {
          if (!format.decodeResponse) return branch(InvalidResponseBranchID,'PROTOCOL','provider did not return the event stream it was asked for',receipt);
          decoded = format.decodeResponse(providerResponse.body);
        }
      } catch (error) {
        if (error instanceof ProviderReportedError) {
          const tokens = safeTokens(error.errorTokens, credential);
          return classifyOutcome(classifyError(format.errorRules ?? [], 0, tokens, true), `provider reported an error inside a 2xx response${tokens.length ? ` ${tokens.join(' ')}` : ''}`, 0, receipt, response, branch, retry);
        }
        if (error instanceof BodyTooLargeError) return branch(InvalidResponseBranchID,'RESPONSE_TOO_LARGE','provider response exceeds the configured size limit',receipt);
        if (error instanceof TextStreamWriteError) return retry('AVAILABILITY','the text stream could not be written');
        return branch(InvalidResponseBranchID,'PROTOCOL','provider response does not match its documented format',receipt);
      }
      if (!boundedIdentifier(decoded.servedModel ?? '') || !boundedIdentifier(decoded.responseId ?? '') || revealsCredential(decoded.servedModel ?? '',credential) || revealsCredential(decoded.responseId ?? '',credential)) return branch(InvalidResponseBranchID,'PROTOCOL','provider returned an invalid model or response identifier',receipt);
      response.servedModel = decoded.servedModel ?? '';
      response.responseId = decoded.responseId ?? '';
      if (response.responseId) receipt.providerObjectId = response.responseId;
      const usage = completeUsage(decoded.usage ?? {});
      if (!usage) return branch(InvalidResponseBranchID,'PROTOCOL','provider reported negative token usage',receipt);
      response.usage = usage;
      const token = decoded.providerFinishReason ?? '';
      if (revealsCredential(token,credential)) return branch(InvalidResponseBranchID,'PROTOCOL','provider returned an invalid finish reason',receipt);
      if (safeToken.test(token)) response.providerFinishReason = token;
      if (decoded.isRefusal) response.finishReason = 'refusal';
      else if (!response.providerFinishReason) return branch(InvalidResponseBranchID,'PROTOCOL','provider returned no recognizable finish reason',receipt);
      else {
        const reason = format.finishReasons[response.providerFinishReason];
        if (!reason) return branch(InvalidResponseBranchID,'PROTOCOL',`provider stopped for an unsupported reason: ${response.providerFinishReason}`,receipt);
        response.finishReason = reason;
      }
      const text = decoded.parts.filter(part => !part.isReasoning).map(part => part.text).join('');
      if (response.finishReason === 'stop') {
        if (!text) return branch(InvalidResponseBranchID,'PROTOCOL','provider finished without text',receipt);
        try { response.text = schema ? extractStructuredAnswerFromSchema(schema,text) : text; }
        catch (error) { return branch(InvalidResponseBranchID,'PROTOCOL',`structured output does not match its schema ${error instanceof Error ? error.message : ''}`,receipt); }
        if (!wasEventStream && call.hasTextStream()) { try { call.writeText(response.text); } catch { return retry('AVAILABILITY','the text stream could not be written'); } }
        return newQueryBranch(GeneratedBranchID,response,receipt);
      }
      if (response.finishReason === 'length') {
        response.text = text;
        if (!wasEventStream && text && call.hasTextStream()) { try { call.writeText(text); } catch { return retry('AVAILABILITY','the text stream could not be written'); } }
        return newQueryBranch(TruncatedBranchID,response,receipt,failure('RESPONSE_TOO_LARGE','provider stopped at the output token limit'));
      }
      return branch(BlockedBranchID,'PROVIDER_REJECTION',response.finishReason === 'refusal' ? 'the model refused the request' : 'provider stopped the response for a content policy',receipt);
    } catch (error) {
      if (error instanceof BodyTooLargeError) return branch(InvalidResponseBranchID,'RESPONSE_TOO_LARGE','provider response exceeds the configured size limit',error.headers ? makeReceipt(call,format,error.headers,credential) : undefined);
      if (error instanceof AddressRefusal) return branch(DefectBranchID,'LOCAL_DEFECT','provider address is not allowed');
      if (error instanceof ProviderStalledError) return retry('AVAILABILITY','provider sent no bytes within the stall timeout');
      if (error instanceof ProviderTransportError) return retry('TRANSPORT','provider request failed before a response was received');
      return retry('TRANSPORT','provider response ended before it was complete');
    } finally { clearInterval(heartbeatTimer); await heartbeat; }
  }
}
/** Construct a validated generateText Query. */
export function newTextGenerationQuery(config: TextGenerationQueryConfig): TextGenerationQuery { return new TextGenerationQuery(config); }
function emptyResponse(): TextGenerationResponse { return { text:'',requestedModel:'',servedModel:'',responseId:'',finishReason:'',providerFinishReason:'',usage:{inputTokens:0,cachedInputTokens:0,outputTokens:0,reasoningTokens:0,totalTokens:0} }; }
function validateFeatures(request: TextGenerationRequest, features: WireFormat['features']): void {
  const fields: [boolean,boolean,string][] = [ [!!request.instructions,!!features.supportsInstructions,'Instructions'], [!!request.structuredOutput,!!features.supportsStructuredOutput,'StructuredOutput'], [!!request.maxOutputTokens,!!features.supportsMaxOutputTokens,'MaxOutputTokens'], [request.temperature !== undefined,!!features.supportsTemperature,'Temperature'], [!!request.reasoningEffort,!!features.supportsReasoningEffort,'ReasoningEffort'] ];
  for (const [used,supported,name] of fields) if (used && !supported) throw new TypeError(`this connector's wire format does not send ${name}; leave it unset`);
}
function validateSampling(request: TextGenerationRequest, rules: ModelRequestRules): void {
  if (request.temperature !== undefined) {
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
  for (const name of format.requestIDHeaders ?? []) { const value = String(headers[name.toLowerCase()] ?? '').trim(); if (boundedIdentifier(value) && value && !revealsCredential(value,credential)) { receipt.providerRequestId = value; break; } }
  for (const name of format.rateLimitHeaders ?? []) { const value = String(headers[name.toLowerCase()] ?? '').trim(); if (value && value.length <= 256 && /^[\x20-\x7e]*$/.test(value) && !revealsCredential(value,credential)) (receipt.metadata ??= {})[name.toLowerCase()] = value; }
  return receipt;
}
function classifyOutcome(outcome: ErrorOutcome, message: string, delay: number, receipt: ReceiptDraft, response: TextGenerationResponse, branch: (id:string,kind:FailureKind,message:string,receipt?:ReceiptDraft)=>QueryAttempt<TextGenerationResponse>, retry: (kind:FailureKind,message:string,delay?:number)=>QueryAttempt<TextGenerationResponse>): QueryAttempt<TextGenerationResponse> {
  if (outcome.disposition === 'retry') return retry(outcome.kind,message,Number.isFinite(delay) ? Math.floor(Math.min(3600000,Math.max(0,delay))) : 0);
  if (outcome.disposition === 'blocked') { response.finishReason = 'contentPolicy'; return branch(BlockedBranchID,outcome.kind,message,receipt); }
  return branch(outcome.disposition === 'invalidResponse' ? InvalidResponseBranchID : ProviderRejectedBranchID,outcome.kind,message,receipt);
}
