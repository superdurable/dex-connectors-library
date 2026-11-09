// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { ZendeskSellClient } from '../src/client.js';
import { GetLeadInputSchema, ListLeadsInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/zendesk/sell/scenarios.json', import.meta.url));
const connection = { provider: 'zendesk-sell', name: 'zendesk-sell' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeZendeskSell, '/v2'));

async function invokeZendeskSell(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { accessToken: SecretString }]> = scenario.name === 'listLeads-defect' ? [] : [[connection, { accessToken: new SecretString('test-token') }]];
  const client = new ZendeskSellClient({ baseUrl: 'http://127.0.0.1:12345/v2' }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'zendesk-sell-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'zendesk-sell', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'listLeads') return client.listLeads().invoke(call, ListLeadsInputSchema.parse(scenario.input));
  if (scenario.operation === 'getLead') return client.getLead().invoke(call, GetLeadInputSchema.parse(scenario.input));
  throw new Error(`unknown ZendeskSell operation ${scenario.operation}`);
}
