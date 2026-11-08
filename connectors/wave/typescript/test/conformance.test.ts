// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { WaveClient } from '../src/client.js';
import { GetCustomerInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/wave/scenarios.json', import.meta.url));
const connection = { provider: 'wave', name: 'wave' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeWave, ''));

async function invokeWave(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { accessToken: SecretString }]> = [[connection, { accessToken: new SecretString('sample-wave-access-token') }]];
  const client = new WaveClient({ baseUrl: 'http://127.0.0.1:12345' }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'wave-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'wave', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getCustomer') return client.getCustomer().invoke(call, GetCustomerInputSchema.parse(scenario.input));
  throw new Error(`unknown Wave operation ${scenario.operation}`);
}
