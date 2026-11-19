// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { JDLogisticsClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/jdlogistics/scenarios.json', import.meta.url));
const connection = { provider: 'jdlogistics', name: 'jdlogistics' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeJDLogistics, ''));

async function invokeJDLogistics(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { appKey: string; appSecret: SecretString; accessToken: SecretString; customerCode: string }]> = [[connection, { appKey: 'app123', appSecret: new SecretString('secretKey'), accessToken: new SecretString('token123'), customerCode: '010K123' }]];
  const client = new JDLogisticsClient({ maxResponseBytes: scenario.name.endsWith('oversized') ? 5 : 65536 }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
    now: () => new Date('2025-01-01T19:04:05Z'),
  });
  const call: Call = {
    context: { flowId: 'jdlogistics-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'jdlogistics', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'queryWaybillTrace') return client.queryWaybillTrace().invoke(call, scenario.input as { waybillNo: string });
  throw new Error(`unknown JDLogistics operation ${scenario.operation}`);
}
