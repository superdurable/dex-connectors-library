// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { YouzanClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/youzan/scenarios.json', import.meta.url));
const connection = { provider: 'youzan', name: 'youzan' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeYouzan, ''));

async function invokeYouzan(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { accessToken: SecretString }]> = [[connection, { accessToken: new SecretString(scenario.name.endsWith('defect') ? '' : 'sample-test-token') }]];
  const client = new YouzanClient({ maxResponseBytes: scenario.name.endsWith('oversized') ? 16 : 262144 }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'youzan-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'youzan', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getShopBasic') return client.getShopBasic().invoke(call);
  throw new Error(`unknown Youzan operation ${scenario.operation}`);
}
