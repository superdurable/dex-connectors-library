// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { ActiveCampaignClient } from '../src/client.js';
import { GetContactInputSchema, ListContactsInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/activecampaign/scenarios.json', import.meta.url));
const connection = { provider: 'activecampaign', name: 'activecampaign' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeActiveCampaign, '/api/3'));

async function invokeActiveCampaign(scenario: Scenario, recorder: ExchangeRecorder) {
  const client = new ActiveCampaignClient({ baseUrl: 'http://127.0.0.1:12345/api/3' }, new StaticCredentialProvider([[connection, { apiKey: new SecretString('test-token') }]]), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'activecampaign-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'activecampaign', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'listContacts') return client.listContacts().invoke(call, ListContactsInputSchema.parse(scenario.input));
  if (scenario.operation === 'getContact') return client.getContact().invoke(call, GetContactInputSchema.parse(scenario.input));
  throw new Error(`unknown ActiveCampaign operation ${scenario.operation}`);
}
