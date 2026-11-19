// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { KokoniWorldClient } from '../src/client.js';
import { ListModelsInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/kokoniworld/scenarios.json', import.meta.url));
const connection = { provider: 'kokoniworld', name: 'kokoniworld' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeKokoniWorld, ''));

async function invokeKokoniWorld(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { apiKey: SecretString }]> = [[connection, { apiKey: new SecretString(scenario.name.endsWith('-defect') ? '' : 'sk-test-secret') }]];
  const client = new KokoniWorldClient({ maxResponseBytes: 1_048_576 }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'kokoniworld-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'kokoniworld', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'listModels') return client.listModels().invoke(call, ListModelsInputSchema.parse(scenario.input));
  throw new Error('unknown KokoniWorld operation ' + scenario.operation);
}
