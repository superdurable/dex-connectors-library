// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newMutationBranch, newMutationRetry, newMutationUncertain, ReauthorizationRequiredError, type Call, type Failure, type FailureKind, type Mutation, type MutationAttempt, type ReceiptDraft } from '@superdurable/connectors-sdk';
import { RefreshFailedError } from '@superdurable/connectors-sdk/projectconfig';
import { AddressRefusal, ProviderTransportError, parseRetryAfter } from '@superdurable/connectors-sdk/providerhttp';
import { SendRequestDefinition } from './generated/definitions.js';
import type { SendRequestInput, SendRequestOutput } from './generated/models.js';
import { planRequest, type APIResponse, type OAuth2HTTPClient, type RequestPlan } from './client.js';

const zeroOutput: SendRequestOutput = { statusCode: 0 };

/** sendRequest Mutation with safe method retries and uncertain writes. */
export class SendRequestOperation implements Mutation<SendRequestInput, SendRequestOutput> {
  /** Bind the connection client. */
  constructor(private readonly client: OAuth2HTTPClient) {}
  /** Return the manifest operation definition. */
  definition() { return SendRequestDefinition; }
  /** Use the stable Call ID as the optional provider idempotency key. */
  idempotencyKey(callId: string): string { return callId; }

  /** Send one OAuth bearer request and classify its observed result. */
  async invoke(call: Call, input: SendRequestInput): Promise<MutationAttempt<SendRequestOutput>> {
    let plan: RequestPlan;
    try { plan = planRequest(this.client, input); }
    catch (error) { return newMutationBranch('defect', zeroOutput, {}, failure('VALIDATION', error instanceof Error ? error.message : 'sendRequest input is invalid')); }
    const deadline = Date.now() + 25_000;
    const signal = AbortSignal.any([call.context.cancellationSignal, AbortSignal.timeout(25_000)]);
    let token: string;
    try {
      const credentials = await this.client.resolveCredentials(call, signal);
      token = credentials.accessToken.reveal();
      if (!token || !credentials.oauthClientId || !credentials.oauthClientSecret.reveal()) return newMutationBranch('defect', zeroOutput, {}, failure('AUTHENTICATION', "the connection's credentials are unavailable or unusable"));
    } catch (error) {
      if (error instanceof AddressRefusal) return newMutationBranch('defect', zeroOutput, {}, failure('VALIDATION', `the connection's tokenEndpoint resolves to an address the connector will not connect to: ${error.reason}`));
      if (error instanceof ReauthorizationRequiredError) return newMutationBranch('rejected', zeroOutput, {}, failure('AUTHENTICATION', 'the OAuth authorization must be renewed in Dex Web Connections'));
      if (error instanceof RefreshFailedError || error instanceof ProviderTransportError || error instanceof Error && /token endpoint is unavailable|token response could not be read/i.test(error.message)) return newMutationRetry(failure('AVAILABILITY', 'the OAuth access token could not be refreshed now'));
      return newMutationBranch('defect', zeroOutput, {}, failure('AUTHENTICATION', "the connection's credentials are unavailable or unusable"));
    }
    let answer: APIResponse;
    let tokens = [token];
    try { answer = await this.client.exchange(call, plan, token, signal, deadline); }
    catch (error) { return dispatchFailure(error, plan, call, this.client.now()); }
    if (answer.status === 401 && !answer.readError) {
      try {
        const replacement = await this.client.resolveAfterRejection(call, signal);
        const replacementToken = replacement.accessToken.reveal();
        if (replacementToken && replacementToken !== token) {
          tokens = [token, replacementToken];
          try { answer = await this.client.exchange(call, plan, replacementToken, signal, deadline); }
          catch (error) { return dispatchFailure(error, plan, call, this.client.now()); }
        }
      } catch { /* The original 401 remains the authoritative API response. */ }
    }
    const output = responseOutput(answer, tokens);
    const receipt: ReceiptDraft = {
      callId: call.id, idempotencyKey: call.idempotencyKey ?? call.id, provider: 'oauth2-http', observedAt: this.client.now().toISOString(),
      ...(safeRequestID(answer.headers['x-request-id']) ? { providerRequestId: safeRequestID(answer.headers['x-request-id']) } : {}),
    };
    const status = answer.status;
    if (answer.readError) {
      if (plan.isSafe) return newMutationRetry(failure('TRANSPORT', `the API did not answer the ${plan.method} request`));
      return newMutationUncertain(output, failure('TRANSPORT', 'the request was sent but the API did not answer completely'), receipt);
    }
    if (status >= 200 && status < 300) return newMutationBranch('completed', output, receipt);
    if (status === 408 || status === 429) return newMutationRetry(failure('RATE_LIMIT', `the API answered HTTP ${status}`), retryAfter(answer, this.client.now()));
    if (status >= 500 && plan.isSafe) return newMutationRetry(failure('AVAILABILITY', `the API answered HTTP ${status}`), retryAfter(answer, this.client.now()));
    if (status >= 500) return newMutationUncertain(output, failure('AVAILABILITY', `the API answered HTTP ${status} after the request was sent`), receipt);
    return newMutationBranch('rejected', output, receipt, failure(rejectionKind(status), `the API answered HTTP ${status}`));
  }
}

function dispatchFailure(error: unknown, plan: RequestPlan, call: Call, now: Date): MutationAttempt<SendRequestOutput> {
  if (error instanceof AddressRefusal) return newMutationBranch('defect', zeroOutput, {}, failure('VALIDATION', `the connection's apiBaseUrl resolves to an address the connector will not connect to: ${error.reason}`));
  if (error instanceof ProviderTransportError && error.isProvablyNotSent) return newMutationRetry(failure('AVAILABILITY', 'the API could not be reached; no request was sent'));
  if (plan.isSafe) return newMutationRetry(failure('TRANSPORT', `the API did not answer the ${plan.method} request`));
  return newMutationUncertain(zeroOutput, failure('TRANSPORT', 'the request was sent but the API did not answer completely'), {
    callId: call.id, idempotencyKey: call.idempotencyKey ?? call.id, provider: 'oauth2-http', observedAt: now.toISOString(),
  });
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

function failure(kind: FailureKind, message: string): Failure { return { kind, provider: 'oauth2-http', operation: 'sendRequest', message }; }
