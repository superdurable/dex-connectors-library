// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { FreshsalesClient } from '../src/client.js';
import { GetContactInputSchema, ListContactViewsInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/freshworks/freshsales/scenarios.json', import.meta.url));
const connection = { provider: 'freshsales', name: 'freshsales' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeFreshsales, '/crm/sales/api'));

async function invokeFreshsales(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { apiKey: SecretString }]> = scenario.name === 'listContactViews-defect' ? [] : [[connection, { apiKey: new SecretString('test-token') }]];
  const client = new FreshsalesClient({ baseUrl: 'http://127.0.0.1:12345/crm/sales/api' }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'freshsales-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'freshsales', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'listContactViews') return client.listContactViews().invoke(call, ListContactViewsInputSchema.parse(scenario.input));
  if (scenario.operation === 'getContact') return client.getContact().invoke(call, GetContactInputSchema.parse(scenario.input));
  throw new Error(`unknown Freshsales operation ${scenario.operation}`);
}
