// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { CopperClient } from '../src/client.js';
import { GetLeadInputSchema, ListLeadStatusesInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/copper/scenarios.json', import.meta.url));
const connection = { provider: 'copper', name: 'copper' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeCopper, '/developer_api/v1'));

async function invokeCopper(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { apiKey: SecretString; userEmail: SecretString }]> = scenario.name === 'listLeadStatuses-defect' ? [] : [[connection, { apiKey: new SecretString('test-token'), userEmail: new SecretString('user@example.com') }]];
  const client = new CopperClient({ baseUrl: 'http://127.0.0.1:12345/developer_api/v1' }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'copper-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'copper', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'listLeadStatuses') return client.listLeadStatuses().invoke(call, ListLeadStatusesInputSchema.parse(scenario.input));
  if (scenario.operation === 'getLead') return client.getLead().invoke(call, GetLeadInputSchema.parse(scenario.input));
  throw new Error(`unknown Copper operation ${scenario.operation}`);
}
