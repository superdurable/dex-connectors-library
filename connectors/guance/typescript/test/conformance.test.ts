// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { GuanceClient } from '../src/client.js';
import { ListDashboardsPageInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/guance/scenarios.json', import.meta.url));
const connection = { provider: 'guance', name: 'guance' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeGuance, ''));

async function invokeGuance(scenario: Scenario, recorder: ExchangeRecorder) {
  const apiKey = 'api-secret';
  const credentials: Array<[typeof connection, { apiKey: SecretString }]> = [[connection, { apiKey: new SecretString(apiKey) }]];
  const client = new GuanceClient({ maxResponseBytes: scenario.name.endsWith('oversized') ? 8 : 1048576 }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'guance-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'guance', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'listDashboardsPage') return client.listDashboardsPage().invoke(call, ListDashboardsPageInputSchema.parse({ pageIndex: 0, pageSize: 0, ...scenario.input }));
  throw new Error(`unknown Guance operation ${scenario.operation}`);
}
