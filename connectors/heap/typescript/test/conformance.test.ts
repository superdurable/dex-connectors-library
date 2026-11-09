// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, invokeProductionMutation, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { HeapClient } from '../src/client.js';
import { SetUserPropertiesInputSchema, type SetUserPropertiesInput } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/heap/scenarios.json', import.meta.url));
const connection = { provider: 'heap', name: 'heap' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeHeap));

function productionMutation<Input, Output>(operation: Mutation<Input, Output>, call: Call, input: Input, key: string) {
  return invokeProductionMutation(operation, key,
    recording => runMutation(recording as Mutation<Input, Output>, call, input), error => error instanceof RetryError);
}

async function invokeHeap(scenario: Scenario, recorder: ExchangeRecorder) {
  const sender = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const client = new HeapClient({ appId: '12345', baseUrl: 'http://127.0.0.1:12345', maxResponseBytes: 8 }, { send: sender });
  const call = scenarioCall(scenario, connection, 'heap', { flowId: 'heap-conformance' },
    { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  if (scenario.operation === 'setUserProperties') return productionMutation(
    client.setUserProperties(), call,
    SetUserPropertiesInputSchema.parse(scenario.input) as SetUserPropertiesInput,
    scenario.idempotencyKey!,
  );
  throw new Error(`unknown Heap operation ${scenario.operation}`);
}
