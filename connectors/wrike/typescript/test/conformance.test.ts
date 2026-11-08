// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { WrikeClient } from '../src/client.js';
import { GetTaskInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/wrike/scenarios.json', import.meta.url));
const connection = { provider: 'wrike', name: 'wrike' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeWrike, '/api'));

async function invokeWrike(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { apiToken: SecretString }]> = [[connection, { apiToken: new SecretString('sample-wrike-api-token') }]];
  const client = new WrikeClient({ baseUrl: 'http://127.0.0.1:12345/api' }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'wrike-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'wrike', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getTask') return client.getTask().invoke(call, GetTaskInputSchema.parse(scenario.input));
  throw new Error(`unknown Wrike operation ${scenario.operation}`);
}
