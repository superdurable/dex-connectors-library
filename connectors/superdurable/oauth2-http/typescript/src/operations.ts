// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { definitionMaxSends, newMutationBranch, newMutationRetry, newMutationUncertain, ReauthorizationRequiredError, type Call, type Failure, type FailureKind, type Mutation, type MutationAttempt, type ReceiptDraft } from '@superdurable/connectors-sdk';
import { RefreshFailedError } from '@superdurable/connectors-sdk/projectconfig';
import { AddressRefusal, ProviderTransportError, parseRetryAfter } from '@superdurable/connectors-sdk/providerhttp';
import { CheckpointError, InsufficientTimeError, newFence, type Fence } from '@superdurable/connectors-sdk/singledispatch';
import { PostNonIdempotentDefinition, SendRequestDefinition } from './generated/definitions.js';
import type { PostNonIdempotentInput, SendRequestInput, SendRequestOutput } from './generated/models.js';
import { planRequest, type APIResponse, type OAuth2HTTPClient, type RequestPlan } from './client.js';

const zeroOutput: SendRequestOutput = { statusCode: 0 };
const sendRequestOperation = 'sendRequest';
const postNonIdempotentOperation = 'postNonIdempotent';
// Only these statuses prove the API refused a request without processing it.
const definiteRefusals = new Set([400, 401, 403, 404, 411, 413, 414, 415, 422]);
// The POST gets most of the Execute budget, because no read-back follows it.
const postWriteWindowFraction = 5 / 6;
const operationBudgetMs = 25_000;

/** sendRequest Mutation: a repeat-safe request that is sent again after every unknown outcome. */
export class SendRequestOperation implements Mutation<SendRequestInput, SendRequestOutput> {
  /** Bind the connection client. */
  constructor(private readonly client: OAuth2HTTPClient) {}
  /** Return the manifest operation definition. */
  definition() { return SendRequestDefinition; }
  /** Use the stable Call ID as the optional provider idempotency key. */
  idempotencyKey(callId: string): string { return callId; }

  /**
   * Send one OAuth bearer request and classify its observed result. A 2xx selects completed; a redirect
   * or a definite refusal selects rejected. Every other outcome returns Retry; uncertain is never selected.
   */
  async invoke(call: Call, input: SendRequestInput): Promise<MutationAttempt<SendRequestOutput>> {
    let plan: RequestPlan;
    try { plan = planRequest(this.client, input); }
    catch (error) { return newMutationBranch('defect', zeroOutput, {}, failure(sendRequestOperation, 'VALIDATION', error instanceof Error ? error.message : 'sendRequest input is invalid')); }
    const deadline = Date.now() + operationBudgetMs;
    const signal = AbortSignal.any([call.context.cancellationSignal, AbortSignal.timeout(operationBudgetMs)]);
    const resolved = await resolveToken(this.client, call, signal, sendRequestOperation);
    if ('attempt' in resolved) return resolved.attempt;
    const token = resolved.token;
    let answer: APIResponse;
    let tokens = [token];
    try { answer = await this.client.exchange(call, plan, token, signal, deadline); }
    catch (error) { return dispatchFailure(error); }
    if (answer.status === 401 && !answer.readError) {
      try {
        const replacement = await this.client.resolveAfterRejection(call, signal);
        const replacementToken = replacement.accessToken.reveal();
        if (replacementToken && replacementToken !== token) {
          tokens = [token, replacementToken];
          try { answer = await this.client.exchange(call, plan, replacementToken, signal, deadline); }
          catch (error) { return dispatchFailure(error); }
        }
      } catch { /* The original 401 remains the authoritative API response. */ }
    }
    const output = responseOutput(answer, tokens);
    const receipt = receiptFor(call, this.client.now(), answer);
    const status = answer.status;
    if (answer.readError) return newMutationRetry(failure(sendRequestOperation, 'TRANSPORT', 'the request was sent but the API did not answer completely'));
    if (status >= 200 && status < 300) return newMutationBranch('completed', output, receipt);
    // The connector never follows a redirect, so a redirect is reported like a refusal.
    if ((status >= 300 && status < 400) || definiteRefusals.has(status)) return newMutationBranch('rejected', output, receipt, failure(sendRequestOperation, rejectionKind(status), `the API answered HTTP ${status}`));
    const delay = retryAfter(answer, this.client.now());
    if (status === 408 || status === 429) return newMutationRetry(failure(sendRequestOperation, 'RATE_LIMIT', `the API answered HTTP ${status}`), delay);
    if (status >= 500) return newMutationRetry(failure(sendRequestOperation, 'AVAILABILITY', `the API answered HTTP ${status}`), delay);
    return newMutationRetry(failure(sendRequestOperation, rejectionKind(status), `the API answered HTTP ${status}, which does not prove the request was refused`), delay);
  }
}

/** postNonIdempotent Mutation: one POST per Step execution under the single-dispatch fence. */
export class PostNonIdempotentOperation implements Mutation<PostNonIdempotentInput, SendRequestOutput> {
  /** Bind the connection client. */
  constructor(private readonly client: OAuth2HTTPClient) {}
  /** Return the manifest operation definition. */
  definition() { return PostNonIdempotentDefinition; }
  /** Use the stable Call ID; the POST does not carry it, and the fence keys on it. */
  idempotencyKey(callId: string): string { return callId; }

  /**
   * Send the POST at most once per Step execution. A 2xx selects completed. A definite refusal of the only
   * send, or an authorization the server will no longer renew, selects rejected. A 429, a 503, a 401 after a
   * renewed token, and a POST that never left this process return Retry. Any other outcome after a possible
   * send, and any attempt after an earlier possible send, selects uncertain and never sends again. An
   * unreadable checkpoint returns Retry.
   */
  async invoke(call: Call, input: PostNonIdempotentInput): Promise<MutationAttempt<SendRequestOutput>> {
    const client = this.client;
    let plan: RequestPlan;
    try {
      plan = planRequest(client, {
        method: 'POST', ...(input.path ? { path: input.path } : {}), ...(input.url ? { url: input.url } : {}),
        ...(input.query ? { query: input.query } : {}), ...(input.headers ? { headers: input.headers } : {}), ...(input.body ? { body: input.body } : {}),
      }, postNonIdempotentOperation);
    } catch (error) {
      return newMutationBranch('defect', zeroOutput, {}, failure(postNonIdempotentOperation, 'VALIDATION', error instanceof Error ? error.message : 'postNonIdempotent input is invalid'));
    }
    let fence: Fence;
    try {
      fence = newFence(call, PostNonIdempotentDefinition.stepDefaults.executeMethodTimeoutMs ?? 30_000, {
        writeWindowFraction: postWriteWindowFraction, maxSends: definitionMaxSends(PostNonIdempotentDefinition),
      });
    } catch {
      return newMutationBranch('defect', zeroOutput, {}, failure(postNonIdempotentOperation, 'LOCAL_DEFECT', 'the single-dispatch fence could not be constructed'));
    }
    const decision = fence.decide();
    if (decision.kind === 'failClosed') return newMutationRetry(failure(postNonIdempotentOperation, 'AVAILABILITY', 'the single-dispatch checkpoint could not be read; nothing was sent in this attempt'));
    if (decision.kind === 'readBackRequired') return newMutationUncertain(zeroOutput, failure(postNonIdempotentOperation, 'TRANSPORT', 'an earlier attempt may have sent the POST, so it is not sent again'), receiptFor(call, client.now()));
    // An earlier send may have been processed, so a refusal of this one cannot prove nothing changed.
    const outcome = new PostOutcome(client, call, fence, (decision.priorSendUpperBound ?? 0) > 0);
    const deadline = Date.now() + operationBudgetMs;
    const signal = AbortSignal.any([call.context.cancellationSignal, AbortSignal.timeout(operationBudgetMs)]);
    const resolved = await resolveToken(client, call, signal, postNonIdempotentOperation);
    if ('attempt' in resolved) {
      const isTerminal = resolved.attempt.kind !== 'retry';
      try { await fence.recordNotSent(); }
      catch {
        if (!isTerminal) return newMutationRetry(failure(postNonIdempotentOperation, 'AVAILABILITY', 'the OAuth access token and the no-send checkpoint are unavailable now'));
      }
      if (isTerminal && outcome.hasPossibleEarlierSend) return outcome.uncertain(zeroOutput, 'AUTHENTICATION', "an earlier attempt may have sent the POST, and the connection's credentials cannot send it now");
      return resolved.attempt;
    }
    const token = resolved.token;
    let answer: APIResponse;
    try {
      answer = await fence.dispatch(dispatchSignal => client.exchange(call, plan, token, AbortSignal.any([signal, dispatchSignal]), deadline));
    } catch (error) {
      if (error instanceof CheckpointError) return newMutationRetry(failure(postNonIdempotentOperation, 'AVAILABILITY', 'the single-dispatch claim could not be recorded; nothing was sent'));
      if (error instanceof InsufficientTimeError) return newMutationRetry(failure(postNonIdempotentOperation, 'AVAILABILITY', "too little of the Step's time remained to send the POST; nothing was sent"));
      if (error instanceof AddressRefusal) return outcome.refuse('defect', zeroOutput, 'VALIDATION', `the connection's apiBaseUrl resolves to an address the connector will not connect to: ${error.reason}`);
      if (error instanceof ProviderTransportError && error.isProvablyNotSent) return outcome.retryUnprocessed('AVAILABILITY', 'the API could not be reached; the POST was not sent', 0);
      return outcome.uncertain(zeroOutput, 'TRANSPORT', 'the POST was sent but the API did not answer completely');
    }
    return outcome.attemptFor(answer, token, signal);
  }
}

/** Classifies the answer to one fenced POST. */
class PostOutcome {
  constructor(
    private readonly client: OAuth2HTTPClient,
    private readonly call: Call,
    private readonly fence: Fence,
    readonly hasPossibleEarlierSend: boolean,
  ) {}

  async attemptFor(answer: APIResponse, token: string, signal: AbortSignal): Promise<MutationAttempt<SendRequestOutput>> {
    const output = responseOutput(answer, [token]);
    const status = answer.status;
    if (answer.readError) return this.uncertain(output, 'TRANSPORT', 'the POST was sent but the API did not answer completely', answer);
    if (status >= 200 && status < 300) return newMutationBranch('completed', output, receiptFor(this.call, this.client.now(), answer));
    if (status === 429) return this.retryUnprocessed('RATE_LIMIT', 'the API answered HTTP 429 without processing the POST', retryAfter(answer, this.client.now()));
    if (status === 503) return this.retryUnprocessed('AVAILABILITY', 'the API answered HTTP 503 without processing the POST', retryAfter(answer, this.client.now()));
    if (status === 401) return this.renewAfterRejection(output, token, answer, signal);
    if (definiteRefusals.has(status)) return this.refuse('rejected', output, rejectionKind(status), `the API answered HTTP ${status}`, answer);
    if (status >= 500) return this.uncertain(output, 'AVAILABILITY', `the API answered HTTP ${status} after the POST was sent`, answer);
    return this.uncertain(output, rejectionKind(status), `the API answered HTTP ${status}, which does not prove the POST was refused`, answer);
  }

  /** Renew after a 401 and return Retry; an authorization that cannot be renewed selects rejected. */
  private async renewAfterRejection(output: SendRequestOutput, token: string, answer: APIResponse, signal: AbortSignal): Promise<MutationAttempt<SendRequestOutput>> {
    let isRenewed: boolean;
    try {
      const replacement = (await this.client.resolveAfterRejection(this.call, signal)).accessToken.reveal();
      isRenewed = Boolean(replacement) && replacement !== token;
    } catch (error) {
      isRenewed = isTemporaryRefreshFailure(error);
    }
    if (isRenewed) return this.retryUnprocessed('AUTHENTICATION', 'the API answered HTTP 401 without processing the POST; it is sent again with a renewed token', 0);
    return this.refuse('rejected', output, 'AUTHENTICATION', 'the API answered HTTP 401 and the OAuth authorization could not be renewed', answer);
  }

  /** Record that the API did not process this send, then return Retry. */
  async retryUnprocessed(kind: FailureKind, message: string, delay: number): Promise<MutationAttempt<SendRequestOutput>> {
    try { await this.fence.recordCanResend(); }
    catch {
      // The claim stays the last checkpoint, so the next attempt selects uncertain instead of sending.
      return newMutationRetry(failure(postNonIdempotentOperation, 'AVAILABILITY', 'the retry checkpoint could not be recorded'));
    }
    return newMutationRetry(failure(postNonIdempotentOperation, kind, message), delay);
  }

  /** Select branch for a refusal of the only send, or uncertain after an earlier possible send. */
  async refuse(branch: 'rejected' | 'defect', output: SendRequestOutput, kind: FailureKind, message: string, answer?: APIResponse): Promise<MutationAttempt<SendRequestOutput>> {
    if (this.hasPossibleEarlierSend) return this.uncertain(output, kind, `${message}; an earlier attempt may have sent the POST`, answer);
    // Best effort: the branch is terminal; a lost canResend only makes a later attempt uncertain.
    try { await this.fence.recordCanResend(); } catch { /* See above. */ }
    return newMutationBranch(branch, output, receiptFor(this.call, this.client.now(), answer), failure(postNonIdempotentOperation, kind, message));
  }

  /** Select uncertain; the claim stays the last checkpoint, so nothing is sent again. */
  uncertain(output: SendRequestOutput, kind: FailureKind, message: string, answer?: APIResponse): MutationAttempt<SendRequestOutput> {
    return newMutationUncertain(output, failure(postNonIdempotentOperation, kind, message), receiptFor(this.call, this.client.now(), answer));
  }
}

/** Resolve usable credentials, or return the attempt for a resolution that failed. */
async function resolveToken(client: OAuth2HTTPClient, call: Call, signal: AbortSignal, operation: string): Promise<{ token: string } | { attempt: MutationAttempt<SendRequestOutput> }> {
  try {
    const credentials = await client.resolveCredentials(call, signal);
    const token = credentials.accessToken.reveal();
    if (!token || !credentials.oauthClientId || !credentials.oauthClientSecret.reveal()) return { attempt: newMutationBranch('defect', zeroOutput, {}, failure(operation, 'AUTHENTICATION', "the connection's credentials are unavailable or unusable")) };
    return { token };
  } catch (error) {
    if (error instanceof AddressRefusal) return { attempt: newMutationBranch('defect', zeroOutput, {}, failure(operation, 'VALIDATION', `the connection's tokenEndpoint resolves to an address the connector will not connect to: ${error.reason}`)) };
    if (error instanceof ReauthorizationRequiredError) return { attempt: newMutationBranch('rejected', zeroOutput, {}, failure(operation, 'AUTHENTICATION', 'the OAuth authorization must be renewed in Dex Web Connections')) };
    if (isTemporaryRefreshFailure(error)) return { attempt: newMutationRetry(failure(operation, 'AVAILABILITY', 'the OAuth access token could not be refreshed now')) };
    return { attempt: newMutationBranch('defect', zeroOutput, {}, failure(operation, 'AUTHENTICATION', "the connection's credentials are unavailable or unusable")) };
  }
}

function isTemporaryRefreshFailure(error: unknown): boolean {
  return error instanceof RefreshFailedError || error instanceof ProviderTransportError || error instanceof Error && /token endpoint is unavailable|token response could not be read/i.test(error.message);
}

function dispatchFailure(error: unknown): MutationAttempt<SendRequestOutput> {
  if (error instanceof AddressRefusal) return newMutationBranch('defect', zeroOutput, {}, failure(sendRequestOperation, 'VALIDATION', `the connection's apiBaseUrl resolves to an address the connector will not connect to: ${error.reason}`));
  if (error instanceof ProviderTransportError && error.isProvablyNotSent) return newMutationRetry(failure(sendRequestOperation, 'AVAILABILITY', 'the API could not be reached; no request was sent'));
  return newMutationRetry(failure(sendRequestOperation, 'TRANSPORT', 'the request was sent but the API did not answer completely'));
}

function receiptFor(call: Call, now: Date, answer?: APIResponse): ReceiptDraft {
  const requestId = answer ? safeRequestID(answer.headers['x-request-id']) : '';
  return { callId: call.id, idempotencyKey: call.idempotencyKey ?? call.id, provider: 'oauth2-http', observedAt: now.toISOString(), ...(requestId ? { providerRequestId: requestId } : {}) };
}

function responseOutput(answer: APIResponse, tokens: string[]): SendRequestOutput {
  const output: SendRequestOutput = { statusCode: answer.status };
  const contentType = String(answer.headers['content-type'] ?? '');
  if (Buffer.byteLength(contentType) <= 256 && /^[\x20-\x7e]*$/.test(contentType) && contentType) output.contentType = contentType;
  let body: Buffer<ArrayBufferLike> = Buffer.from(answer.body);
  if (answer.truncated) {
    body = trimIncompleteUTF8(body);
    output.bodyTruncated = true;
  }
  for (const token of tokens) if (Buffer.byteLength(token) >= 8) body = Buffer.from(body.toString('latin1').split(Buffer.from(token).toString('latin1')).join('[REDACTED]'), 'latin1');
  try { output.body = new TextDecoder('utf-8', { fatal: true }).decode(body); }
  catch { output.body = body.toString('base64'); output.bodyIsBase64 = true; }
  if (!output.body) delete output.body;
  return output;
}

function trimIncompleteUTF8(body: Buffer): Buffer {
  if (!body.length) return body;
  let start = body.length - 1;
  while (start > 0 && (body[start]! & 0xc0) === 0x80 && body.length - start < 4) start--;
  const first = body[start]!;
  const expected = first < 0x80 ? 1 : first >= 0xc2 && first <= 0xdf ? 2 : first >= 0xe0 && first <= 0xef ? 3 : first >= 0xf0 && first <= 0xf4 ? 4 : 0;
  return expected === 0 || body.length - start < expected ? body.subarray(0, start) : body;
}

function safeRequestID(value: string | string[] | undefined): string {
  const text = (Array.isArray(value) ? value[0] ?? '' : value ?? '').trim();
  return /^[A-Za-z0-9._:-]{1,128}$/.test(text) ? text : '';
}

function retryAfter(answer: APIResponse, now: Date): number {
  const value = answer.headers['retry-after'];
  return parseRetryAfter(Array.isArray(value) ? value[0] ?? '' : value ?? '', now);
}

function rejectionKind(status: number): FailureKind {
  if (status === 401) return 'AUTHENTICATION';
  if (status === 403) return 'AUTHORIZATION';
  if (status === 404 || status === 410) return 'NOT_FOUND';
  if (status === 409) return 'CONFLICT';
  if (status === 400 || status === 422) return 'VALIDATION';
  return 'PROVIDER_REJECTION';
}

function failure(operation: string, kind: FailureKind, message: string): Failure { return { kind, provider: 'oauth2-http', operation, message }; }
