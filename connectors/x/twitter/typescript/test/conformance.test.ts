// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { XClient } from '../src/client.js';
import { GetPostInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/x/twitter/scenarios.json', import.meta.url));
const connection = { provider: 'x-twitter', name: 'x-twitter' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeX, '/2'));

async function invokeX(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { accessToken: SecretString }]> = [[connection, { accessToken: new SecretString('sample-x-access-token') }]];
  const client = new XClient({ baseUrl: 'http://127.0.0.1:12345/2' }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'x-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'x-twitter', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getPost') return client.getPost().invoke(call, GetPostInputSchema.parse(scenario.input));
  throw new Error(`unknown X operation ${scenario.operation}`);
}
