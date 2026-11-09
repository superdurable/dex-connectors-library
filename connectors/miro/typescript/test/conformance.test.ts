// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { MiroClient } from '../src/client.js';
import { GetBoardInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/miro/scenarios.json', import.meta.url));
const connection = { provider: 'miro', name: 'miro' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeMiro, '/v2'));

async function invokeMiro(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { accessToken: SecretString }]> = [[connection, { accessToken: new SecretString('sample-miro-access-token') }]];
  const client = new MiroClient({ baseUrl: 'http://127.0.0.1:12345/v2' }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'miro-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'miro', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getBoard') return client.getBoard().invoke(call, GetBoardInputSchema.parse(scenario.input));
  throw new Error(`unknown Miro operation ${scenario.operation}`);
}
