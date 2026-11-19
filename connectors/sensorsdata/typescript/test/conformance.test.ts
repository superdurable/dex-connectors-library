// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { SensorsDataClient } from '../src/client.js';
import { ListCampaignsInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/sensorsdata/scenarios.json', import.meta.url));
const connection = { provider: 'sensorsdata', name: 'sensorsdata' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeSensorsData, ''));

async function invokeSensorsData(scenario: Scenario, recorder: ExchangeRecorder) {
  const apiKey = scenario.name.endsWith('invalid-credentials') ? 'bad key' : 'k'.repeat(35);
  const credentials: Array<[typeof connection, { apiKey: SecretString }]> = [[connection, { apiKey: new SecretString(apiKey) }]];
  const client = new SensorsDataClient({ baseUrl: 'http://127.0.0.1:12345', projectName: 'production', maxResponseBytes: scenario.name.endsWith('oversize') ? 8 : 2097152 }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'sensorsdata-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'sensorsdata', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'listCampaigns') return client.listCampaigns().invoke(call, ListCampaignsInputSchema.parse(scenario.input));
  throw new Error(`unknown Sensors Data operation ${scenario.operation}`);
}
