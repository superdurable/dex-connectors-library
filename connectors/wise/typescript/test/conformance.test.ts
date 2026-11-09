// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { WiseClient } from '../src/client.js';
import { GetTransferInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/wise/scenarios.json', import.meta.url));
const connection = { provider: 'wise', name: 'wise' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeWise, '/api'));

async function invokeWise(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { apiToken: SecretString }]> = [[connection, { apiToken: new SecretString('sample-wise-api-token') }]];
  const client = new WiseClient({ baseUrl: 'http://127.0.0.1:12345/api' }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'wise-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'wise', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getTransfer') return client.getTransfer().invoke(call, GetTransferInputSchema.parse(scenario.input));
  throw new Error(`unknown Wise operation ${scenario.operation}`);
}
