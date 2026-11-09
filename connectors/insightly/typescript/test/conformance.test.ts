// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { InsightlyClient } from '../src/client.js';
import { GetContactInputSchema, ListContactsInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/insightly/scenarios.json', import.meta.url));
const connection = { provider: 'insightly', name: 'insightly' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeInsightly, '/v3.1'));

async function invokeInsightly(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { apiKey: SecretString }]> = scenario.name === 'listContacts-defect' ? [] : [[connection, { apiKey: new SecretString('test-token') }]];
  const client = new InsightlyClient({ baseUrl: 'http://127.0.0.1:12345/v3.1' }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'insightly-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'insightly', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'listContacts') return client.listContacts().invoke(call, ListContactsInputSchema.parse(scenario.input));
  if (scenario.operation === 'getContact') return client.getContact().invoke(call, GetContactInputSchema.parse(scenario.input));
  throw new Error(`unknown Insightly operation ${scenario.operation}`);
}
