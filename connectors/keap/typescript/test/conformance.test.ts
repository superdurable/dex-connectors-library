// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { KeapClient } from '../src/client.js';
import { GetContactInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/keap/scenarios.json', import.meta.url));
const connection = { provider: 'keap', name: 'keap' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeKeap, ''));

async function invokeKeap(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { personalAccessToken: SecretString }]> = [[connection, { personalAccessToken: new SecretString('sample-keap-personal-access-token') }]];
  const client = new KeapClient({ baseUrl: 'http://127.0.0.1:12345' }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'keap-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'keap', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getContact') return client.getContact().invoke(call, GetContactInputSchema.parse(scenario.input));
  throw new Error(`unknown Keap operation ${scenario.operation}`);
}
