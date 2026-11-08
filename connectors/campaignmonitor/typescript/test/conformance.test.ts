// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { CampaignMonitorClient } from '../src/client.js';
import { ListClientsInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/campaignmonitor/scenarios.json', import.meta.url));
const connection = { provider: 'campaignmonitor', name: 'campaignmonitor' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeCampaignMonitor, '/api/v3.3'));

async function invokeCampaignMonitor(scenario: Scenario, recorder: ExchangeRecorder) {
  const client = new CampaignMonitorClient({ baseUrl: 'http://127.0.0.1:12345/api/v3.3' }, new StaticCredentialProvider(scenario.name === 'listClients-defect' ? [] : [[connection, { apiKey: new SecretString('test-token') }]]), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'campaignmonitor-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'campaignmonitor', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'listClients') return client.listClients().invoke(call, ListClientsInputSchema.parse(scenario.input));
  throw new Error(`unknown Campaign Monitor operation ${scenario.operation}`);
}
