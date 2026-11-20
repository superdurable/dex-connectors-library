// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { QinceClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/qince/scenarios.json', import.meta.url));
const connection = { provider: 'qince', name: 'qince' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeQince, ''));

async function invokeQince(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'listDepartments') throw new Error(`unknown Qince operation ${scenario.operation}`);
  const isDefect = scenario.name.endsWith('-defect');
  const credentials = new StaticCredentialProvider([[connection, { openId: isDefect ? '' : '8519670967275282420', appKey: new SecretString('2EV5i30kYI6r7Hz817') }]]);
  const client = new QinceClient({ baseUrl: 'https://openapi.region2.qince.com', ...(scenario.name.endsWith('-oversized') ? { maxResponseBytes: 16 } : {}) }, credentials, { messageID: () => '11111111111111111111111111111111', now: () => new Date('2024-11-13T15:00:00Z'), send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)) });
  const call: Call = {
    context: { flowId: 'qince-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'qince', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.listDepartments().invoke(call, scenario.input as unknown as Parameters<ReturnType<QinceClient['listDepartments']>['invoke']>[1]);
}
