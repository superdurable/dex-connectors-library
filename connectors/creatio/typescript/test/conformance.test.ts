// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { CreatioClient } from '../src/client.js';
import { GetContactInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/creatio/scenarios.json', import.meta.url));
const connection = { provider: 'creatio', name: 'creatio' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeCreatio, ''));

async function invokeCreatio(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { clientId: string; clientSecret: SecretString; accessToken: SecretString }]> = [[connection, { clientId: 'sample-client-id', clientSecret: new SecretString('sample-client-secret'), accessToken: new SecretString('sample-access-token') }]];
  const client = new CreatioClient({ baseUrl: 'http://127.0.0.1:12345', tokenEndpoint: 'http://127.0.0.1:12345/connect/token' }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'creatio-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'creatio', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getContact') return client.getContact().invoke(call, GetContactInputSchema.parse(scenario.input));
  throw new Error(`unknown Creatio operation ${scenario.operation}`);
}
