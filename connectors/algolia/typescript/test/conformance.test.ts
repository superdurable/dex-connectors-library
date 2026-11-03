// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { Client, SaveObjectInputSchema, SearchInputSchema, type SaveObjectInput } from '../src/index.js';
import { answerRecordedRequest, ExchangeRecorder, invokeProductionMutation, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/algolia/scenarios.json', import.meta.url));
const connection = { provider: 'algolia', name: 'algolia' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeAlgolia, '/1'));

async function invokeAlgolia(scenario: Scenario, recorder: ExchangeRecorder) {
  const sender = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const credentials = { resolve: async () => ({ applicationId: 'TESTAPP', apiKey: new SecretString('algolia-test-key') }) };
  const client = new Client({ baseUrl: 'http://127.0.0.1:12345/1' }, credentials, sender);
  const call = scenarioCall(scenario, connection, 'algolia', { flowId: 'algolia-conformance', cancellationSignal: new AbortController().signal }, {
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  }) as Call;
  if (scenario.operation === 'search') return client.search().invoke(call, SearchInputSchema.parse(scenario.input));
  if (scenario.operation === 'saveObject') {
    const operation = client.saveObject();
    return invokeProductionMutation(operation, scenario.idempotencyKey!, recording =>
      runMutation(recording as Mutation<SaveObjectInput, unknown>, call, SaveObjectInputSchema.parse(scenario.input)), error => error instanceof RetryError);
  }
  throw new Error(`unknown operation ${scenario.operation}`);
}
