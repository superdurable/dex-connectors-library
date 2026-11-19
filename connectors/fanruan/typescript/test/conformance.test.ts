// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { FanruanClient } from '../src/client.js';
import { ListAppsPageInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/fanruan/scenarios.json', import.meta.url));
const connection = { provider: 'fanruan', name: 'fanruan' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeFanruan, ''));

async function invokeFanruan(scenario: Scenario, recorder: ExchangeRecorder) {
  const apiKey = scenario.name.endsWith('missing-credentials') ? '' : 'secret-api-key';
  const credentials: Array<[typeof connection, { apiKey: SecretString }]> = [[connection, { apiKey: new SecretString(apiKey) }]];
  const client = new FanruanClient({ maxResponseBytes: scenario.name.endsWith('oversize') ? 8 : 1048576 }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'fanruan-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'fanruan', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'listAppsPage') return client.listAppsPage().invoke(call, ListAppsPageInputSchema.parse(scenario.input));
  throw new Error(`unknown Fanruan operation ${scenario.operation}`);
}
