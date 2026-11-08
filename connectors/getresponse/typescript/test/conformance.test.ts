// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { GetResponseClient } from '../src/client.js';
import { GetContactInputSchema, ListContactsInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/getresponse/scenarios.json', import.meta.url));
const connection = { provider: 'getresponse', name: 'getresponse' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeGetResponse, '/v3'));

async function invokeGetResponse(scenario: Scenario, recorder: ExchangeRecorder) {
  const client = new GetResponseClient({ baseUrl: 'http://127.0.0.1:12345/v3' }, new StaticCredentialProvider([[connection, { apiKey: new SecretString('test-token') }]]), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'getresponse-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'getresponse', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'listContacts') return client.listContacts().invoke(call, ListContactsInputSchema.parse(scenario.input));
  if (scenario.operation === 'getContact') return client.getContact().invoke(call, GetContactInputSchema.parse(scenario.input));
  throw new Error(`unknown GetResponse operation ${scenario.operation}`);
}
