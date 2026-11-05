// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import {
  Client, Registry, StepList, Stream, Worker, forceComplete, openBlobCache, stringCodec,
  type Context, type Flow, type Step, type StepDecision,
} from '@superdurable/dex';
import { goToBranch, newQueryStep } from '../../step.js';
import type { QueryResult } from '../../types.js';
import type { TextGenerationRequest, TextGenerationResponse } from '../contract.js';
import type { TextGenerationQuery } from '../query.js';
import { FakeProvider } from './fake_provider.js';
import type { FakeConnection, ProviderDialect } from './exchange_suite.js';

/** Real Dex Worker/Client scenarios for one provider's ordinary Query factory. */
export interface TextGenerationDexScenarioSuite {
  /** Provider wire dialect shared with the exchange suite. */ dialect: ProviderDialect;
  /** Build the ordinary connector Query with a request timeout of at least 15 seconds. */
  newQuery(connection: FakeConnection): TextGenerationQuery | Promise<TextGenerationQuery>;
  /** Dex FlowService address; defaults to DEX_SERVER_ADDRESS or 127.0.0.1:18891. */ serverAddress?: string;
}

class CompleteTextGeneration implements Step<QueryResult<TextGenerationResponse>> {
  constructor(readonly stepType: string) {}
  getStepType(): string { return this.stepType; }
  execute(_context: Context, result: QueryResult<TextGenerationResponse>): StepDecision {
    return forceComplete(result);
  }
}

/** Exercise retry, model precedence, Streams, heartbeat, unwired branches, and Worker loss through Dex. */
export async function runTextGenerationDexScenarios(suite: TextGenerationDexScenarioSuite): Promise<void> {
  if (!suite?.dialect || !suite.newQuery) throw new TypeError('text generation Dex scenario suite is incomplete');
  const credential = 'sk-textgentest-dex-canary';
  const provider = await FakeProvider.start(suite.dialect.credentialHeader, credential);
  const cacheDirectory = await mkdtemp(join(tmpdir(), 'textgentest-dex-'));
  const cache = openBlobCache({ directory: cacheDirectory, maxBytes: 64 << 20 });
  let client: Client | undefined;
  let worker: Worker | undefined;
  try {
    const query = await suite.newQuery({
      baseURL: provider.baseURL(), model: suite.dialect.connectionModel,
      apiKey: credential, maxResponseBytes: 64 << 10, requestTimeoutMs: 15_000,
    });
    const unique = randomUUID().replaceAll('-', '');
    const textStream = new Stream(`textgentest-text-${unique}`, stringCodec, 1 << 20);
    const done = new CompleteTextGeneration(`TextgentestDone${unique}`);
    const step = newQueryStep<TextGenerationRequest, TextGenerationRequest, TextGenerationResponse>({
      stepType: `TextgentestGenerate${unique}`,
      annotations: { groupId: 'textgentest', groupLabel: 'Text Generation Test', explanation: 'Generate a fixture answer.' },
      connection: { provider: 'textgentest', name: 'default' },
      mapToOperationInput: input => input, operation: query,
      branches: [goToBranch('generated', done)], textStream,
      stepOptionsOverride: {
        executeDurability: 'sync', executeMethodTimeoutMs: 20_000, heartbeatTimeoutMs: 10_000,
        executeRetry: { initialIntervalMs: 1000, backoffCoefficient: 1, maximumIntervalMs: 1000,
          maximumAttempts: 3, totalDurationMs: 30_000 },
      },
    });
    const flow: Flow<TextGenerationRequest> = {
      getFlowType: () => `TextgentestFlow${unique}`,
      getSteps: () => StepList.startStep(step).otherSteps(done),
      getPersistenceSchema: () => ({ streams: [textStream] }),
    };
    const registry = new Registry([flow]);
    const serverAddress = suite.serverAddress ?? process.env.DEX_SERVER_ADDRESS ?? '127.0.0.1:18891';
    const workerOptions = { serverAddress, pull: { maxConcurrentTasks: 4, reservedRpcTasks: 0, stopDrainTimeoutMs: 100 } };
    worker = new Worker(registry, cache, workerOptions);
    client = new Client(registry, cache, { serverAddress });
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 10_000 });

    const completion = { text: 'The textgentest Dex answer.', servedModel: 'textgentest-served', responseId: 'textgentest-dex-1',
      usage: { inputTokens: 11, cachedInputTokens: 3, outputTokens: 7, reasoningTokens: 2, totalTokens: 18 } };
    provider.enqueueReplies(suite.dialect.errorReply(429, 'private retry message'), {
      ...suite.dialect.generatedReply(completion), delayMs: 11_000,
    });
    const firstFlowID = `textgentest-${randomUUID()}`;
    const input: TextGenerationRequest = { model: suite.dialect.alternateModel, messages: [{ role: 'user', text: 'Say hello.' }] };
    await client.startFlow(flow, firstFlowID, input);
    const generated = await client.waitForFlow(firstFlowID, 35_000);
    assert.equal(generated.status, 'completed', generated.errorMessage ?? 'text generation did not complete');
    const generatedResult = generated.singleOutput<QueryResult<TextGenerationResponse>>().value;
    assert.equal(generatedResult.text, completion.text);
    assert.equal(generatedResult.requestedModel, suite.dialect.alternateModel);
    assert.equal(provider.requests().length, 2);
    const firstRequest = provider.requests()[0];
    const secondRequest = provider.requests()[1];
    assert.ok(firstRequest && secondRequest);
    assert.equal(suite.dialect.readRequestModel(firstRequest), suite.dialect.alternateModel);
    assert.equal(suite.dialect.readRequestModel(secondRequest), suite.dialect.alternateModel);
    assert.equal(firstRequest.hasCredentialInSlot && secondRequest.hasCredentialInSlot, true);
    const textMessages = await waitForTextMessages(client, firstFlowID, textStream);
    assert.ok(textMessages.some(message => message.includes(completion.text)));

    provider.enqueueReplies(suite.dialect.truncatedReply({ ...completion, text: 'partial' }));
    const secondFlowID = `textgentest-${randomUUID()}`;
    await client.startFlow(flow, secondFlowID, { messages: [{ role: 'user', text: 'Continue.' }] });
    const truncated = await client.waitForFlow(secondFlowID, 10_000);
    assert.equal(truncated.status, 'failed');
    assert.equal(provider.requests().length, 3);

    let expectedRequestsBeforeWorkerLoss = 3;
    if (suite.dialect.interruptedStreamReply) {
      const partial = 'partial from interrupted attempt';
      provider.enqueueReplies(suite.dialect.interruptedStreamReply({ ...completion, text: partial }),
        suite.dialect.generatedReply(completion));
      const interruptedFlowID = `textgentest-${randomUUID()}`;
      await client.startFlow(flow, interruptedFlowID, { messages: [{ role: 'user', text: 'Retry the stream.' }] });
      const completedAfterRetry = await client.waitForFlow(interruptedFlowID, 15_000);
      assert.equal(completedAfterRetry.status, 'completed');
      assert.equal(completedAfterRetry.singleOutput<QueryResult<TextGenerationResponse>>().value.text, completion.text);
      const text = await waitForTextMessages(client, interruptedFlowID, textStream, 2);
      assert.equal(text.reverse().join(''), partial + completion.text);
      expectedRequestsBeforeWorkerLoss += 2;
      assert.equal(provider.requests().length, expectedRequestsBeforeWorkerLoss);
    }

    provider.enqueueReplies({ ...suite.dialect.generatedReply(completion), delayMs: 60_000 },
      suite.dialect.generatedReply(completion));
    const lostWorkerFlowID = `textgentest-${randomUUID()}`;
    await client.startFlow(flow, lostWorkerFlowID, { messages: [{ role: 'user', text: 'Recover.' }] });
    await waitForRecordedRequests(provider, expectedRequestsBeforeWorkerLoss + 1);
    await worker.close();
    worker = new Worker(registry, cache, workerOptions);
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 10_000 });
    const recovered = await client.waitForFlow(lostWorkerFlowID, 35_000);
    assert.equal(recovered.status, 'completed', recovered.errorMessage ?? 'lost Worker request did not recover');
    assert.equal(recovered.singleOutput<QueryResult<TextGenerationResponse>>().value.text, completion.text);
    assert.equal(provider.requests().length, expectedRequestsBeforeWorkerLoss + 2);
  } finally {
    if (client) await client.close();
    if (worker) await worker.close();
    cache.close();
    await rm(cacheDirectory, { recursive: true, force: true });
    await provider.close();
  }
}

async function waitForRecordedRequests(provider: FakeProvider, count: number): Promise<void> {
  const deadline = Date.now() + 10_000;
  while (Date.now() < deadline) {
    if (provider.requests().length >= count) return;
    await new Promise<void>(resolve => setTimeout(resolve, 50));
  }
  throw new Error('fake provider did not receive the expected request');
}

async function waitForTextMessages(client: Client, flowID: string, stream: Stream<string>, expectedCount = 1): Promise<string[]> {
  const deadline = Date.now() + 5000;
  while (Date.now() < deadline) {
    const page = await client.listStreamMessages(flowID, stream, 10);
    if (page.messages.length >= expectedCount) return page.messages.map(message => message.value);
    await new Promise<void>(resolve => setTimeout(resolve, 100));
  }
  throw new Error('text generation Stream did not converge');
}
