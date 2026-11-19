// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, invokeProductionMutation, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { PineconeClient } from '../src/client.js';
import { QueryVectorsInputSchema, UpsertVectorsInputSchema, type UpsertVectorsInput, type UpsertVectorsOutput } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/pinecone/scenarios.json', import.meta.url));
const connection = { provider: 'pinecone', name: 'pinecone' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokePinecone));

async function invokePinecone(scenario: Scenario, recorder: ExchangeRecorder) {
  const sender = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const client = new PineconeClient({ baseUrl: 'http://127.0.0.1:12345' }, {
    resolve: async () => ({ apiKey: new SecretString('pinecone-test-key') }),
  }, { send: sender });
  const call = scenarioCall(scenario, connection, 'pinecone', {
    flowId: 'pinecone-conformance', cancellationSignal: new AbortController().signal,
  }, { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  if (scenario.operation === 'queryVectors') return client.queryVectors().invoke(call, QueryVectorsInputSchema.parse(scenario.input));
  if (scenario.operation === 'upsertVectors') {
    const operation: Mutation<UpsertVectorsInput, UpsertVectorsOutput> = client.upsertVectors();
    return invokeProductionMutation(operation, scenario.idempotencyKey!, recording =>
      runMutation(recording as Mutation<UpsertVectorsInput, UpsertVectorsOutput>, call, UpsertVectorsInputSchema.parse(scenario.input)),
    error => error instanceof RetryError);
  }
  throw new Error(`unknown Pinecone operation ${scenario.operation}`);
}
