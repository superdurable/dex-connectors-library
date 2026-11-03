// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import assert from 'node:assert/strict';
import type { AsyncContext } from '@superdurable/dex';
import { createCall } from '../../credentials.js';
import type { TextGenerationRequest, Usage } from '../contract.js';
import type { TextGenerationQuery } from '../query.js';
import type { CredentialHeader } from '../wire_format.js';
import { FakeProvider, type FakeReply, type RecordedRequest } from './fake_provider.js';

const credentialCanary = 'sk-textgentest-credential-canary';
const providerMessageCanary = 'textgentest-private-provider-message';
const generatedText = 'The textgentest answer.';
const partialText = 'The partial textgentest';
const reportedUsage: Usage = { inputTokens: 11, cachedInputTokens: 3, outputTokens: 7, reasoningTokens: 2, totalTokens: 18 };

/** Values the provider-specific dialect must render for the exchange suite. */
export interface GeneratedReply {
  /** User-visible answer. */ text: string;
  /** Provider's model echo. */ servedModel: string;
  /** Provider response identity. */ responseId: string;
  /** Distinct token counts for field mapping checks. */ usage: Usage;
}

/** Provider-specific wire rendering and request parsing for shared exchange cases. */
export interface ProviderDialect {
  /** Declared credential location. */ credentialHeader: CredentialHeader;
  /** Connection's configured model. */ connectionModel: string;
  /** Different valid request model. */ alternateModel: string;
  /** Optional response header read into the receipt. */ requestIDHeader?: string;
  /** Read the sent model from one sanitized request. */ readRequestModel(request: RecordedRequest): string;
  /** Render a normal completion with all usage counts. */ generatedReply(reply: GeneratedReply): FakeReply;
  /** Render an output-limit stop. */ truncatedReply(reply: GeneratedReply): FakeReply;
  /** Render a content-policy stop or refusal. */ blockedReply(reply: GeneratedReply): FakeReply;
  /** Render one provider status and private message. */ errorReply(statusCode: number, message: string): FakeReply;
  /** Render provider quota exhaustion. */ quotaExhaustedReply(message: string): FakeReply;
  /** Render malformed 2xx output. */ malformedReply(): FakeReply;
  /** Render a 2xx embedded error when supported. */ reportedErrorReply?(token: string, message: string): FakeReply;
  /** Render an interrupted event stream when the dialect streams. */ interruptedStreamReply?(reply: GeneratedReply): FakeReply;
  /** Render a complete body for a streaming request when supported. */ unstreamedReply?(reply: GeneratedReply): FakeReply;
  /** JSON pointers whose optional fields must be absent in the basic request. */ optionalRequestFieldPointers?: readonly string[];
}

/** Loopback connection passed to a connector's ordinary query factory. */
export interface FakeConnection {
  /** Fake provider base URL. */ baseURL: string;
  /** Configured provider model. */ model: string;
  /** Canary API key; never put it in Flow state. */ apiKey: string;
  /** Successful-response byte limit. */ maxResponseBytes: number;
  /** Optional minimum exchange timeout for a silent-provider Dex scenario. */ requestTimeoutMs?: number;
}

/** One shared exchange suite invocation for a connector dialect. */
export interface TextGenerationExchangeSuite {
  /** Provider-specific request and response shapes. */ dialect: ProviderDialect;
  /** Build the connector's normal generateText Query. */ newQuery(connection: FakeConnection): TextGenerationQuery | Promise<TextGenerationQuery>;
  /** Connector-specific invalid requests that must cause zero provider calls. */ locallyRejectedRequests?: readonly TextGenerationRequest[];
}

/** Run Go-equivalent model, branch, error, safety, and stream exchanges. */
export async function runTextGenerationExchangeSuite(suite: TextGenerationExchangeSuite): Promise<void> {
  const dialect = suite?.dialect;
  if (!dialect?.credentialHeader.name || !dialect.connectionModel || !dialect.alternateModel || !suite.newQuery) {
    throw new TypeError('text generation exchange suite is incomplete');
  }
  const provider = await FakeProvider.start(dialect.credentialHeader, credentialCanary);
  try {
    const query = await suite.newQuery({ baseURL: provider.baseURL(), model: dialect.connectionModel, apiKey: credentialCanary, maxResponseBytes: 64 << 10 });
    const completion: GeneratedReply = { text: generatedText, servedModel: 'textgentest-served-model', responseId: 'textgentest-response-1', usage: reportedUsage };
    const basic = (): TextGenerationRequest => ({ messages: [{ role: 'user', text: 'Say hello to textgentest.' }] });
    const invoke = (request: TextGenerationRequest) => query.invoke(createCall({
      flowId: 'textgentest-flow', stepExecutionId: 'generate:1', attempt: 1, recordHeartbeat: async () => {},
    } as unknown as AsyncContext, { provider: 'textgentest', name: 'default' }, query.definition().operation), request);
    const assertSafeRequest = (request: RecordedRequest) => {
      assert.equal(request.method, 'POST');
      assert.equal(request.hasCredentialInSlot, true);
      assert.equal(request.hasCredentialOutsideSlot, false);
      assert.equal(request.body.includes(credentialCanary), false);
    };

    provider.enqueueReplies(dialect.generatedReply(completion));
    const generated = await invoke(basic());
    assert.equal(generated.kind, 'branch');
    if (generated.kind !== 'branch') throw new Error('generated reply retried');
    assert.equal(generated.branch, 'generated');
    assert.equal(generated.value.text, generatedText);
    assert.equal(generated.value.requestedModel, dialect.connectionModel);
    assert.equal(generated.value.servedModel, completion.servedModel);
    assert.equal(generated.value.responseId, completion.responseId);
    assert.deepEqual(generated.value.usage, reportedUsage);
    const firstRequest = provider.requests()[0];
    assert.ok(firstRequest);
    assertSafeRequest(firstRequest);
    assert.equal(dialect.readRequestModel(firstRequest), dialect.connectionModel);
    if (dialect.requestIDHeader) assert.ok(generated.receipt.providerRequestId);
    for (const pointer of dialect.optionalRequestFieldPointers ?? []) assert.equal(resolveJSONPointer(firstRequest.body, pointer), undefined);

    provider.enqueueReplies(dialect.generatedReply(completion));
    const alternate = await invoke({ ...basic(), model: dialect.alternateModel });
    assert.equal(alternate.kind, 'branch');
    if (alternate.kind === 'branch') assert.equal(alternate.branch, 'generated');
    const secondRequest = provider.requests()[1];
    assert.ok(secondRequest);
    assertSafeRequest(secondRequest);
    assert.equal(dialect.readRequestModel(secondRequest), dialect.alternateModel);

    provider.enqueueReplies(dialect.truncatedReply({ ...completion, text: partialText }));
    const truncated = await invoke(basic());
    assert.equal(truncated.kind, 'branch');
    if (truncated.kind === 'branch') {
      assert.equal(truncated.branch, 'truncated');
      assert.equal(truncated.value.text, partialText);
    }

    provider.enqueueReplies(dialect.blockedReply(completion));
    const blocked = await invoke(basic());
    assert.equal(blocked.kind, 'branch');
    if (blocked.kind === 'branch') { assert.equal(blocked.branch, 'blocked'); assert.equal(blocked.value.text, ''); }

    for (const status of [429, 503]) {
      provider.enqueueReplies(dialect.errorReply(status, providerMessageCanary));
      const outcome = await invoke(basic());
      assert.equal(outcome.kind, 'retry');
      if (outcome.kind === 'retry') assert.equal(outcome.failure.kind, status === 429 ? 'RATE_LIMIT' : 'AVAILABILITY');
      assert.equal(outcome.failure?.message.includes(providerMessageCanary), false);
    }

    provider.enqueueReplies(dialect.errorReply(401, providerMessageCanary));
    const rejected = await invoke(basic());
    assert.equal(rejected.kind, 'branch');
    if (rejected.kind === 'branch') assert.equal(rejected.branch, 'providerRejected');

    provider.enqueueReplies(dialect.errorReply(403, providerMessageCanary));
    const forbidden = await invoke(basic());
    assert.equal(forbidden.kind, 'branch');
    if (forbidden.kind === 'branch') {
      assert.equal(forbidden.branch, 'providerRejected');
      assert.equal(forbidden.failure?.kind, 'AUTHORIZATION');
    }

    provider.enqueueReplies(dialect.quotaExhaustedReply(providerMessageCanary));
    const quota = await invoke(basic());
    assert.equal(quota.kind, 'branch');
    if (quota.kind === 'branch') {
      assert.equal(quota.branch, 'providerRejected');
      assert.equal(quota.failure?.kind, 'QUOTA_EXHAUSTED');
    }

    const retryWithSeconds = dialect.errorReply(429, providerMessageCanary);
    provider.enqueueReplies({ ...retryWithSeconds, headers: { ...retryWithSeconds.headers, 'Retry-After': '7' } });
    const seconds = await invoke(basic());
    assert.equal(seconds.kind, 'retry');
    if (seconds.kind === 'retry') assert.equal(seconds.retryAfterMs, 7000);

    const retryWithDate = dialect.errorReply(429, providerMessageCanary);
    provider.enqueueReplies({ ...retryWithDate, headers: {
      ...retryWithDate.headers, 'Retry-After': new Date(Date.now() + 30_000).toUTCString(),
    } });
    const dated = await invoke(basic());
    assert.equal(dated.kind, 'retry');
    if (dated.kind === 'retry') {
      assert.ok(dated.retryAfterMs >= 25_000);
      assert.ok(dated.retryAfterMs <= 30_000);
    }

    for (const status of [500, 502, 504]) {
      provider.enqueueReplies(dialect.errorReply(status, providerMessageCanary));
      const failure = await invoke(basic());
      assert.equal(failure.kind, 'retry');
      if (failure.kind === 'retry') assert.equal(failure.failure.kind, 'AVAILABILITY');
    }
    provider.enqueueReplies({ statusCode: 503, headers: { 'Content-Type': 'text/html' },
      body: `<html>${providerMessageCanary}</html>` });
    const htmlGateway = await invoke(basic());
    assert.equal(htmlGateway.kind, 'retry');
    assert.equal(htmlGateway.failure?.message.includes(providerMessageCanary), false);

    const requestCountBeforeRedirect = provider.requests().length;
    provider.enqueueReplies({ statusCode: 307, headers: { Location: `${provider.baseURL()}/redirected` } });
    const redirect = await invoke(basic());
    assert.equal(redirect.kind, 'branch');
    if (redirect.kind === 'branch') assert.equal(redirect.branch, 'invalidResponse');
    assert.equal(provider.requests().length, requestCountBeforeRedirect + 1);

    provider.enqueueReplies({ shouldDropConnection: true });
    const dropped = await invoke(basic());
    assert.equal(dropped.kind, 'retry');
    if (dropped.kind === 'retry') assert.equal(dropped.failure.kind, 'TRANSPORT');

    provider.enqueueReplies(dialect.malformedReply());
    const malformed = await invoke(basic());
    assert.equal(malformed.kind, 'branch');
    if (malformed.kind === 'branch') assert.equal(malformed.branch, 'invalidResponse');

    provider.enqueueReplies(dialect.generatedReply({ ...completion, text: 'x'.repeat(64 << 10) }));
    const oversized = await invoke(basic());
    assert.equal(oversized.kind, 'branch');
    if (oversized.kind === 'branch') {
      assert.equal(oversized.branch, 'invalidResponse');
      assert.equal(oversized.failure?.kind, 'RESPONSE_TOO_LARGE');
    }

    if (query.requestFeatures().supportsStructuredOutput) {
      const schema = { type: 'object', additionalProperties: false, required: ['answer', 'score'], properties: {
        answer: { type: 'string' }, score: { type: 'integer', maximum: 10 },
      } };
      const structuredRequest = (selectedSchema: Record<string, unknown>): TextGenerationRequest => ({
        ...basic(), structuredOutput: { name: 'textgentest_answer', schema: selectedSchema },
      });
      const matching = '{"answer":"forty-two","score":7}';
      provider.enqueueReplies(dialect.generatedReply({ ...completion, text: matching }));
      const structured = await invoke(structuredRequest(schema));
      assert.equal(structured.kind, 'branch');
      if (structured.kind === 'branch') {
        assert.equal(structured.branch, 'generated', structured.failure?.message);
        assert.equal(structured.value.text, matching);
      }
      provider.enqueueReplies(dialect.generatedReply({ ...completion, text: `\`\`\`json\n${matching}\n\`\`\`` }));
      const fenced = await invoke(structuredRequest(schema));
      assert.equal(fenced.kind, 'branch');
      if (fenced.kind === 'branch') {
        assert.equal(fenced.branch, 'generated');
        assert.equal(fenced.value.text, matching);
      }
      provider.enqueueReplies(dialect.generatedReply({ ...completion, text: '{"answer":"forty-two","score":42}' }));
      const mismatched = await invoke(structuredRequest(schema));
      assert.equal(mismatched.kind, 'branch');
      if (mismatched.kind === 'branch') {
        assert.equal(mismatched.branch, 'invalidResponse');
        assert.ok(mismatched.failure?.message.includes('/score'));
      }
      const invalidSchema = { ...schema, properties: { ...schema.properties,
        answer: { anyOf: [{ type: 'string' }, { type: 'integer' }] },
      } };
      const beforeInvalidSchema = provider.requests().length;
      const invalid = await invoke(structuredRequest(invalidSchema));
      assert.equal(invalid.kind, 'branch');
      if (invalid.kind === 'branch') assert.equal(invalid.branch, 'defect');
      assert.equal(provider.requests().length, beforeInvalidSchema);
    }

    const invalidModelRequest: TextGenerationRequest = { ...basic(), model: 'invalid model id' };
    const countBeforeInvalidModel = provider.requests().length;
    const invalidModel = await invoke(invalidModelRequest);
    assert.equal(invalidModel.kind, 'branch');
    if (invalidModel.kind === 'branch') assert.equal(invalidModel.branch, 'defect');
    assert.equal(provider.requests().length, countBeforeInvalidModel);

    if (dialect.reportedErrorReply) {
      provider.enqueueReplies(dialect.reportedErrorReply('textgentest_unmatched_error', providerMessageCanary));
      const reported = await invoke(basic());
      assert.equal(reported.kind, 'branch');
      if (reported.kind === 'branch') assert.equal(reported.branch, 'invalidResponse');
      assert.equal(reported.failure?.message.includes(providerMessageCanary), false);
    }
    if (dialect.interruptedStreamReply) {
      provider.enqueueReplies(dialect.interruptedStreamReply(completion));
      const interrupted = await invoke(basic());
      assert.equal(interrupted.kind, 'retry');
      if (interrupted.kind === 'retry') assert.equal(interrupted.failure.kind, 'TRANSPORT');
    }
    if (dialect.unstreamedReply) {
      provider.enqueueReplies(dialect.unstreamedReply(completion));
      const unstreamed = await invoke(basic());
      assert.equal(unstreamed.kind, 'branch');
      if (unstreamed.kind === 'branch') assert.equal(unstreamed.branch, 'generated');
    }
    for (const rejectedRequest of suite.locallyRejectedRequests ?? []) {
      const before = provider.requests().length;
      const local = await invoke(rejectedRequest);
      assert.equal(local.kind, 'branch');
      if (local.kind === 'branch') assert.equal(local.branch, 'defect');
      assert.equal(provider.requests().length, before);
    }
  } finally { await provider.close(); }
}

function resolveJSONPointer(body: Buffer, pointer: string): unknown {
  if (!pointer.startsWith('/')) throw new TypeError('optional request pointer must begin with /');
  let value: unknown = JSON.parse(body.toString());
  for (const encoded of pointer.slice(1).split('/')) {
    const segment = encoded.replaceAll('~1', '/').replaceAll('~0', '~');
    if (value === null || typeof value !== 'object') return undefined;
    value = (value as Record<string, unknown>)[segment];
  }
  return value;
}
