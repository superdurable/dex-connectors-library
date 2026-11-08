// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { PersonioClient } from '../src/client.js';
import { GetPersonInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/personio/scenarios.json', import.meta.url));
const connection = { provider: 'personio', name: 'personio' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokePersonio, '/v2'));

async function invokePersonio(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { clientId: string; clientSecret: SecretString; accessToken: SecretString }]> = [[connection, { clientId: 'sample-client-id', clientSecret: new SecretString('sample-client-secret'), accessToken: new SecretString('sample-access-token') }]];
  const client = new PersonioClient({ baseUrl: 'http://127.0.0.1:12345/v2' }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'personio-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'personio', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getPerson') return client.getPerson().invoke(call, GetPersonInputSchema.parse(scenario.input));
  throw new Error(`unknown Personio operation ${scenario.operation}`);
}
