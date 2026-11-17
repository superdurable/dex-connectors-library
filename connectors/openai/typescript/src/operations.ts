// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { definitionMaxSends, newMutationBranch, newMutationRetry, newMutationUncertain, newQueryBranch, newQueryRetry, zeroReceipt, type Call, type Failure, type FailureKind, type Mutation, type MutationAttempt, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, ProviderTransportError, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import type { CreateRequest, Response, RetrieveRequest } from './generated/models.js';
import { CreateResponseDefinition, RetrieveResponseDefinition } from './generated/definitions.js';
import { CredentialFailure, EncodingFailure, isPrintableModelID, isSafeResponseID, openAIFailure, pathEscape, type OpenAIClient } from './client.js';

const emptyResponse: Response = { id: '', model: '', status: '', outputText: '', usage: { inputTokens: 0, cachedInputTokens: 0, outputTokens: 0, reasoningOutputTokens: 0, totalTokens: 0 } };
const invalidCreateInputMessage = 'input and a model of printable ASCII without spaces are required';

/** Heartbeat checkpoint; `sendCount` bounds possibly effective creates and is absent in earlier releases. */
interface Checkpoint { callId: string; attempt: number; responseId?: string; canResend?: boolean; sendCount?: number }

/** Possibly effective creates of one call across attempts, bounded as the single-dispatch Fence does. */
class SendBudget {
  constructor(readonly priorSendUpperBound: number, private readonly maxSends: number) {}
  /** Trust a valid send count, add one possible send per later attempt, and otherwise use attempt - 1. */
  static derive(call: Call, previous: Checkpoint | undefined, isTrusted: boolean): SendBudget {
    const attempt = call.context.attempt;
    const operationMaxSends = definitionMaxSends(CreateResponseDefinition);
    const maxSends = operationMaxSends >= 2 ? call.maxSendsOverride ?? operationMaxSends : 1;
    const count = isTrusted ? previous?.sendCount : undefined;
    const isValid = previous !== undefined && Number.isSafeInteger(count) && Number.isSafeInteger(previous.attempt) &&
      previous.attempt >= 1 && previous.attempt < attempt && (count as number) >= 0 && (count as number) <= previous.attempt;
    return new SendBudget(isValid ? (count as number) + attempt - 1 - previous.attempt : attempt - 1, maxSends);
  }
  get canSendNow(): boolean { return this.priorSendUpperBound < this.maxSends; }
  get canResendLater(): boolean { return this.priorSendUpperBound + 1 < this.maxSends; }
  get hasPossibleEarlierSend(): boolean { return this.priorSendUpperBound > 0; }
}

const definiteRefusalStatuses = new Set([400, 401, 403, 404, 411, 413, 414, 415, 422]);

class CheckpointHeartbeat {
  private checkpoint: Checkpoint | undefined;
  private timer: ReturnType<typeof setInterval> | undefined;
  private pending: Promise<void> = Promise.resolve();
  constructor(private readonly call: Call) {}
  async begin(checkpoint: Checkpoint): Promise<void> {
    try { await this.record(checkpoint); }
    finally {
      this.timer = setInterval(() => {
        if (this.checkpoint) this.pending = this.pending.then(async () => {
          try { if (this.checkpoint) await this.call.context.recordHeartbeat(this.checkpoint); }
          catch { /* A later heartbeat can still refresh the checkpoint. */ }
        });
      }, 5000);
    }
  }
  async record(checkpoint: Checkpoint): Promise<void> { this.checkpoint = checkpoint; await this.call.context.recordHeartbeat(checkpoint); }
  /** Record the response ID, keeping the claim's send count. */
  async responseId(id: string): Promise<void> { await this.record({ callId: this.call.id, attempt: this.call.context.attempt, responseId: id, ...(this.sendCount === undefined ? {} : { sendCount: this.sendCount }) }); }
  /** Record that the next attempt may resend, with sendCount possible creates through this attempt. */
  async canResend(sendCount: number): Promise<void> { await this.record({ callId: this.call.id, attempt: this.call.context.attempt, canResend: true, sendCount }); }
  /** The send count of the checkpoint this attempt last recorded. */
  get sendCount(): number | undefined { return this.checkpoint?.sendCount; }
  async stop(): Promise<void> { if (this.timer) clearInterval(this.timer); await this.pending; }
}

/** Stored OpenAI response creation with heartbeat checkpoint recovery. */
export class CreateResponseOperation implements Mutation<CreateRequest, Response> {
  /** Bind one client. */ constructor(private readonly client: OpenAIClient) {}
  /** Return manifest-defined operation metadata. */ definition() { return CreateResponseDefinition; }
  /** Return the stable Call ID used for correlation, not provider deduplication. */ idempotencyKey(callId: string): string { return callId; }

  /** Send once or read back a checkpointed response on a later attempt. */
  async invoke(call: Call, input: CreateRequest): Promise<MutationAttempt<Response>> {
    const model = input.Model?.trim() || this.client.model;
    if (input.Input === null || input.Input === undefined || !isPrintableModelID(model)) return this.branch(call, 'defect', emptyResponse, 'VALIDATION', invalidCreateInputMessage, false);
    const payload: Record<string, unknown> = { model, input: input.Input };
    if (input.Instructions) payload.instructions = input.Instructions;
    if (input.StructuredOutput) payload.text = { format: { type: 'json_schema', name: input.StructuredOutput.name, description: input.StructuredOutput.description ?? '', schema: input.StructuredOutput.schema, strict: input.StructuredOutput.strict } };
    const streaming = call.hasProgressStream() || call.hasTextStream();
    if (streaming) payload.stream = true;
    let previous: Checkpoint | undefined;
    let isTrusted = true;
    try {
      const raw = call.context.hasLastHeartbeatValue() ? call.context.getLastHeartbeatValue() : undefined;
      previous = raw == null ? undefined : raw as Checkpoint;
    } catch { isTrusted = false; previous = undefined; }
    if (previous !== undefined && (typeof previous !== 'object' || Array.isArray(previous) || previous.callId !== call.id)) { isTrusted = false; previous = undefined; }
    if (!isTrusted && call.context.attempt === 1) return this.branch(call, 'defect', emptyResponse, 'LOCAL_DEFECT', "the Step's heartbeat value is not this call's checkpoint; createResponse must be the Step's only heartbeat writer", false);
    // A later attempt treats an unreadable or foreign checkpoint, or an invalid saved ID, as none.
    if (previous?.responseId && !isSafeResponseID(previous.responseId)) { isTrusted = false; previous = undefined; }
    const sends = SendBudget.derive(call, previous, isTrusted);
    const heartbeat = new CheckpointHeartbeat(call);
    try {
      if (previous?.responseId) {
        try { await heartbeat.begin({ callId: call.id, attempt: call.context.attempt, responseId: previous.responseId, sendCount: sends.priorSendUpperBound }); }
        catch { /* The earlier checkpoint already holds the ID, so a failed record changes nothing. */ }
        return await this.readBack(call, heartbeat, previous.responseId);
      }
      if (sends.canSendNow) return await this.sendCreate(call, heartbeat, sends, payload, streaming);
      return newMutationUncertain(emptyResponse, openAIFailure('TRANSPORT', 'createResponse', 'earlier attempts may have created responses without recording an ID, and the send cap is reached'), this.client.receipt(call, undefined));
    } finally { await heartbeat.stop(); }
  }

  private async sendCreate(call: Call, heartbeat: CheckpointHeartbeat, sends: SendBudget, payload: Record<string, unknown>, streaming: boolean): Promise<MutationAttempt<Response>> {
    const stream = streaming ? new ResponseStream(call, heartbeat, this.client) : undefined;
    let prepared: ProviderRequest;
    try {
      prepared = await this.client.prepareRequest(call, 'POST', '/responses', payload, stream?.onChunk);
      if (stream) prepared = { ...prepared, onBodyLimit: 'drain' };
    }
    catch (error) {
      const [kind, message]: [FailureKind, string] = error instanceof CredentialFailure ? ['AUTHENTICATION', error.message] : error instanceof EncodingFailure ? ['VALIDATION', error.message] : ['LOCAL_DEFECT', 'request could not be built'];
      // An earlier create may exist, so a defect could prompt a second billed create.
      if (sends.hasPossibleEarlierSend) return newMutationRetry(openAIFailure(kind, 'createResponse', message));
      return this.branch(call, 'defect', emptyResponse, kind, message, false);
    }
    try { await heartbeat.begin({ callId: call.id, attempt: call.context.attempt, sendCount: sends.priorSendUpperBound + 1 }); }
    catch { return this.retry(heartbeat, sends.priorSendUpperBound, 'AVAILABILITY', 'dispatch checkpoint could not be recorded'); }
    let response: ProviderResponse;
    try { response = await this.client.sendPreparedRequest(prepared); }
    catch (error) {
      if (error instanceof ProviderTransportError && error.isProvablyNotSent) return this.retry(heartbeat, sends.priorSendUpperBound, 'TRANSPORT', 'request was not sent');
      if (stream?.responseId) { await stream.waitForCheckpoint(); return this.readBack(call, heartbeat, stream.responseId, stream.responseHeaders ? { status: stream.responseStatus, headers: stream.responseHeaders, body: new Uint8Array() } : undefined); }
      if (error instanceof BodyTooLargeError) return resendOrSelectUncertain(sends, openAIFailure('RESPONSE_TOO_LARGE', 'createResponse', 'provider response exceeds the configured size limit'), this.client.receipt(call, { status: error.status ?? 200, headers: error.headers ?? {}, body: new Uint8Array() }));
      return resendOrSelectUncertain(sends, openAIFailure('TRANSPORT', 'createResponse', 'provider outcome is unknown'), this.client.receipt(call, undefined));
    }
    if (response.status < 200 || response.status >= 300) {
      const classified = classifyStatus('createResponse', response, this.client);
      if (classified.retry) return this.retry(heartbeat, sends.priorSendUpperBound, classified.kind, classified.message, classified.retryAfter);
      if (response.status === 408 || response.status === 409) return resendOrSelectUncertain(sends, openAIFailure(response.status === 408 ? 'TRANSPORT' : 'CONFLICT', 'createResponse', `provider returned HTTP ${response.status} without a response ID, so a response may exist`), this.client.receipt(call, response));
      // A 5xx other than 503, a 3xx, or an unlisted 4xx can follow a stored response.
      if (!definiteRefusalStatuses.has(response.status)) return resendOrSelectUncertain(sends, openAIFailure(classified.kind, 'createResponse', classified.message), this.client.receipt(call, response));
      try { await heartbeat.canResend(sends.priorSendUpperBound); } catch { /* A lost record leaves the claim, which only overcounts. */ }
      // A refused resend cannot prove that an earlier attempt created nothing.
      if (sends.hasPossibleEarlierSend) return newMutationUncertain(emptyResponse, openAIFailure(classified.kind, 'createResponse', classified.message), this.client.receipt(call, response));
      return this.branch(call, 'providerRejected', emptyResponse, classified.kind, classified.message, true, response);
    }
    if (stream) return stream.finish(response, sends, this.readBack.bind(this), this.resolveKnown.bind(this));
    let result: Response;
    try { result = this.client.decodeResponse(response.body); }
    catch (error) { return resendOrSelectUncertain(sends, openAIFailure(error instanceof BodyTooLargeError ? 'RESPONSE_TOO_LARGE' : 'PROTOCOL', 'createResponse', error instanceof BodyTooLargeError ? 'provider response exceeds the configured size limit' : 'provider response is invalid'), this.client.receipt(call, response)); }
    if (!result.id || !isSafeResponseID(result.id)) return resendOrSelectUncertain(sends, openAIFailure('PROTOCOL', 'createResponse', result.id ? 'provider response ID is invalid' : 'provider response has no ID'), this.client.receipt(call, response));
    try { await heartbeat.responseId(result.id); } catch { /* A lost checkpoint leaves the claim, which a later attempt counts. */ }
    return this.resolveKnown(call, heartbeat, result, response);
  }

  private async resolveKnown(call: Call, heartbeat: CheckpointHeartbeat, result: Response, response?: ProviderResponse): Promise<MutationAttempt<Response>> {
    const receipt = this.client.receipt(call, response, result.id);
    if (result.status === 'completed') return newMutationBranch('completed', result, receipt);
    if (result.status === 'failed' || result.status === 'incomplete') return newMutationBranch('failed', result, receipt, openAIFailure('PROVIDER_REJECTION', 'createResponse', `provider returned a ${result.status} response`));
    // The cancelled response may be billed, so it still counts toward the send cap.
    if (result.status === 'cancelled') return this.retry(heartbeat, heartbeat.sendCount ?? call.context.attempt, 'AVAILABILITY', `response ${result.id} was cancelled before it completed`);
    if (result.status === 'queued' || result.status === 'in_progress') return this.readBack(call, heartbeat, result.id, response);
    return newMutationUncertain(result, openAIFailure('PROTOCOL', 'createResponse', `response ${result.id} has no recognized status`), receipt);
  }

  private async readBack(call: Call, heartbeat: CheckpointHeartbeat, responseId: string, createResponse?: ProviderResponse): Promise<MutationAttempt<Response>> {
    const deadline = (call.context.deadline?.getTime() ?? Date.now() + 120_000) - 5000;
    let interval = 1000;
    for (;;) {
      if (call.context.cancellationSignal.aborted) return newMutationRetry(openAIFailure('AVAILABILITY', 'createResponse', `response ${responseId} read-back was interrupted`));
      let read: ProviderResponse;
      try { read = await this.client.request(call, 'GET', `/responses/${pathEscape(responseId)}`, undefined, undefined, Math.min(10_000, Math.max(1, deadline - Date.now()))); }
      catch (error) {
        if (error instanceof CredentialFailure) return newMutationRetry(openAIFailure('AUTHENTICATION', 'createResponse', `response ${responseId} read-back request could not be built`));
        if (error instanceof BodyTooLargeError) return newMutationUncertain({ ...emptyResponse, id: responseId }, openAIFailure('RESPONSE_TOO_LARGE', 'createResponse', `response ${responseId} read-back is unreadable: provider response exceeds the configured size limit`), this.client.receipt(call, createResponse, responseId));
        read = { status: 0, headers: {}, body: new Uint8Array() };
      }
      if (read.status >= 200 && read.status < 300) {
        let result: Response;
        try { result = this.client.decodeResponse(read.body); }
        catch { return newMutationUncertain({ ...emptyResponse, id: responseId }, openAIFailure('PROTOCOL', 'createResponse', `response ${responseId} read-back is unreadable: provider response is invalid`), this.client.receipt(call, createResponse, responseId)); }
        if (result.id !== responseId) return newMutationUncertain({ ...emptyResponse, id: responseId }, openAIFailure('PROTOCOL', 'createResponse', `response ${responseId} read-back returned another response ID`), this.client.receipt(call, createResponse, responseId));
        if (result.status !== 'queued' && result.status !== 'in_progress') return this.resolveKnown(call, heartbeat, result, createResponse);
      } else if ([401, 403, 404].includes(read.status) || (read.status !== 0 && read.status !== 408 && read.status !== 409 && read.status !== 429 && read.status < 500)) {
        const kind: FailureKind = read.status === 401 ? 'AUTHENTICATION' : read.status === 403 ? 'AUTHORIZATION' : read.status === 404 ? 'NOT_FOUND' : 'PROVIDER_REJECTION';
        const message = read.status === 404 ? 'was not found on read-back, so its outcome cannot be confirmed' : read.status === 401 ? 'read-back returned HTTP 401; the API key must be valid and allowed to read responses with GET /v1/responses/{id}' : read.status === 403 ? 'read-back returned HTTP 403; the API key needs read permission for responses (GET /v1/responses/{id})' : `read-back returned HTTP ${read.status}`;
        return newMutationUncertain({ ...emptyResponse, id: responseId }, openAIFailure(kind, 'createResponse', `response ${responseId} ${message}`), this.client.receipt(call, createResponse, responseId));
      }
      const retryAfter = read.status === 429 ? this.client.retryAfter(read) : 0;
      const wait = Math.max(interval, retryAfter);
      if (Date.now() + wait > deadline) return newMutationRetry(openAIFailure('AVAILABILITY', 'createResponse', `response ${responseId} is not confirmed yet`), retryAfter);
      await new Promise(resolve => setTimeout(resolve, wait));
      interval = Math.min(interval * 2, 5000);
    }
  }

  /** Let the next attempt send again; call it only when this send created nothing to repeat. */
  private async retry(heartbeat: CheckpointHeartbeat, sendCount: number, kind: FailureKind, message: string, retryAfter = 0): Promise<MutationAttempt<Response>> {
    try { await heartbeat.canResend(sendCount); } catch { /* A lost record leaves the claim, which only overcounts. */ }
    return newMutationRetry(openAIFailure(kind, 'createResponse', message), retryAfter);
  }
  private branch(call: Call, branch: string, value: Response, kind: FailureKind, message: string, withReceipt: boolean, response?: ProviderResponse): MutationAttempt<Response> {
    return newMutationBranch(branch, value, withReceipt ? this.client.receipt(call, response) : zeroReceipt(), openAIFailure(kind, 'createResponse', message));
  }
}

/** Stored OpenAI response retrieval Query. */
export class RetrieveResponseOperation implements Query<RetrieveRequest, Response> {
  /** Bind one client. */ constructor(private readonly client: OpenAIClient) {}
  /** Return manifest-defined operation metadata. */ definition() { return RetrieveResponseDefinition; }
  /** Read a response by ID and classify one provider exchange. */
  async invoke(call: Call, input: RetrieveRequest): Promise<QueryAttempt<Response>> {
    if (!input.ResponseID) return newQueryBranch('defect', emptyResponse, zeroReceipt(), openAIFailure('VALIDATION', 'retrieveResponse', 'response ID is required'));
    let response: ProviderResponse;
    try { response = await this.client.request(call, 'GET', `/responses/${pathEscape(input.ResponseID)}`); }
    catch (error) {
      if (error instanceof CredentialFailure) return newQueryBranch('defect', emptyResponse, zeroReceipt(), openAIFailure('AUTHENTICATION', 'retrieveResponse', error.message));
      if (error instanceof BodyTooLargeError) return newQueryBranch('invalidResponse', emptyResponse, this.client.receipt(call, { status: error.status ?? 200, headers: error.headers ?? {}, body: new Uint8Array() }, input.ResponseID), openAIFailure('RESPONSE_TOO_LARGE', 'retrieveResponse', 'provider response exceeds the configured size limit'));
      return newQueryRetry(openAIFailure('AVAILABILITY', 'retrieveResponse', 'provider is unavailable'));
    }
    if (response.status < 200 || response.status >= 300) {
      const classified = classifyStatus('retrieveResponse', response, this.client);
      if (classified.retry) return newQueryRetry(openAIFailure(classified.kind, 'retrieveResponse', classified.message), classified.retryAfter);
      return newQueryBranch(response.status === 404 ? 'notFound' : 'providerRejected', emptyResponse, this.client.receipt(call, response, input.ResponseID), openAIFailure(classified.kind, 'retrieveResponse', classified.message));
    }
    let result: Response;
    try { result = this.client.decodeResponse(response.body); }
    catch { return newQueryBranch('invalidResponse', emptyResponse, this.client.receipt(call, response, input.ResponseID), openAIFailure('PROTOCOL', 'retrieveResponse', 'provider response is invalid')); }
    return newQueryBranch('found', result, this.client.receipt(call, response, result.id));
  }
}

/** Retry while a later attempt may create again, and select uncertain at the cap. */
function resendOrSelectUncertain(sends: SendBudget, failure: Failure, receipt: ReturnType<OpenAIClient['receipt']>): MutationAttempt<Response> {
  // The claim stays the last checkpoint, so the next attempt counts this send.
  return sends.canResendLater ? newMutationRetry(failure) : newMutationUncertain(emptyResponse, failure, receipt);
}

function classifyStatus(operation: string, response: ProviderResponse, client: OpenAIClient): { kind: FailureKind; message: string; retry: boolean; retryAfter: number } {
  const status = response.status;
  const kind: FailureKind = status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : status === 404 ? 'NOT_FOUND' : status === 408 || status === 503 || status >= 500 ? 'AVAILABILITY' : status === 409 ? 'CONFLICT' : status === 429 ? 'RATE_LIMIT' : 'PROVIDER_REJECTION';
  return { kind, message: `provider returned HTTP ${status}`, retry: status === 429 || status === 503 || (operation === 'retrieveResponse' && (status === 408 || status >= 500)), retryAfter: status === 429 ? client.retryAfter(response) : 0 };
}

class ResponseStream {
  responseId = '';
  responseStatus = 0;
  responseHeaders: ProviderResponse['headers'] | undefined;
  private pendingCheckpoint: Promise<void> = Promise.resolve();
  private readonly decoder = new TextDecoder();
  private source = '';
  private totalBytes = 0;
  private error = '';
  private terminal: MutationAttempt<Response> | undefined;
  private isDeliveryStopped = false;
  constructor(private readonly call: Call, private readonly heartbeat: CheckpointHeartbeat, private readonly client: OpenAIClient) {}
  async waitForCheckpoint(): Promise<void> { await this.pendingCheckpoint; }
  readonly onChunk: NonNullable<ProviderRequest['onResponseChunk']> = (chunk, status, headers) => {
    this.responseStatus = status;
    this.responseHeaders = headers;
    if (status < 200 || status >= 300) return true;
    this.totalBytes += chunk.length;
    if (this.totalBytes > this.client.maxResponseBytes) { this.error = 'stream exceeds the configured size limit'; return true; }
    this.source += this.decoder.decode(chunk, { stream: true });
    for (;;) {
      const boundary = /\r?\n\r?\n/.exec(this.source);
      if (!boundary) break;
      const block = this.source.slice(0, boundary.index);
      this.source = this.source.slice(boundary.index + boundary[0].length);
      if (Buffer.byteLength(block) > this.client.maxSseEventBytes) { this.error = 'stream event exceeds the configured size limit'; return true; }
      const data = block.split(/\r?\n/).filter(line => line.startsWith('data:')).map(line => line.slice(5).trim()).join('\n');
      if (!data) continue;
      let event: Record<string, unknown>;
      try { event = JSON.parse(data) as Record<string, unknown>; } catch { continue; }
      const wire = event.response;
      if (wire && typeof wire === 'object' && 'id' in wire && typeof wire.id === 'string' && wire.id && !this.responseId) {
        if (!isSafeResponseID(wire.id)) { this.error = 'provider response ID is invalid'; return true; }
        this.responseId = wire.id;
        this.pendingCheckpoint = this.heartbeat.responseId(wire.id).catch(() => { /* A lost checkpoint cannot authorize a duplicate create. */ });
      }
      if (event.type === 'error') { this.error = 'provider streamed an error before naming a response, which may exist'; continue; }
      if (!this.isDeliveryStopped && typeof event.type === 'string') {
        try {
          if (['response.created', 'response.queued', 'response.in_progress'].includes(event.type)) this.call.reportProgress({ phase: event.type, message: typeof (wire as Record<string, unknown> | undefined)?.status === 'string' ? String((wire as Record<string, unknown>).status) : '', ...(typeof event.sequence_number === 'number' ? { providerSequence: event.sequence_number } : {}) });
          if (event.type === 'response.output_text.delta' && typeof event.delta === 'string') this.call.writeText(event.delta);
        } catch { this.isDeliveryStopped = true; }
      }
      if (['response.completed', 'response.failed', 'response.incomplete'].includes(String(event.type))) {
        try {
          const result = this.client.decodeResponse(Buffer.from(JSON.stringify(wire)));
          if (result.id && result.id === this.responseId) this.terminal = event.type === 'response.completed' ? newMutationBranch('completed', result, this.client.receipt(this.call, undefined, result.id)) : newMutationBranch('failed', result, this.client.receipt(this.call, undefined, result.id), openAIFailure('PROVIDER_REJECTION', 'createResponse', `provider returned ${event.type}`));
        } catch { /* A later terminal event or read-back may still establish the outcome. */ }
      }
    }
    return this.terminal === undefined;
  };
  async finish(response: ProviderResponse, sends: SendBudget, readBack: (call: Call, heartbeat: CheckpointHeartbeat, responseId: string, createResponse?: ProviderResponse) => Promise<MutationAttempt<Response>>, resolveKnown: (call: Call, heartbeat: CheckpointHeartbeat, result: Response, response?: ProviderResponse) => Promise<MutationAttempt<Response>>): Promise<MutationAttempt<Response>> {
    await this.pendingCheckpoint;
    if (this.terminal) {
      const result = this.terminal;
      if (result.kind === 'branch') return resolveKnown(this.call, this.heartbeat, result.value, response);
      return result;
    }
    if (this.responseId) return readBack(this.call, this.heartbeat, this.responseId, response);
    return resendOrSelectUncertain(sends, openAIFailure('PROTOCOL', 'createResponse', this.error || 'stream ended before a terminal event'), this.client.receipt(this.call, response));
  }
}
