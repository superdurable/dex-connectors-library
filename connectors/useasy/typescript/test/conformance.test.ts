// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { UseasyClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/useasy/scenarios.json', import.meta.url));
const connection = { provider: 'useasy', name: 'useasy' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeUseasy, ''));

async function invokeUseasy(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'listAgents') throw new Error(`unknown Useasy operation ${scenario.operation}`);
  const isDefect = scenario.name.endsWith('-defect');
  const credentials = new StaticCredentialProvider([[connection, { appId: new SecretString(isDefect ? '' : '1011'), secretKey: new SecretString('private-test-key') }]]);
  const client = new UseasyClient({ ...(scenario.name.endsWith('-oversized') ? { maxResponseBytes: 16 } : {}) }, credentials, { now: () => new Date('2024-11-13T15:00:00Z'), nonce: () => '123456', send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)) });
  const call: Call = {
    context: { flowId: 'useasy-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'useasy', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.listAgents().invoke(call, scenario.input as unknown as Parameters<ReturnType<UseasyClient['listAgents']>['invoke']>[1]);
}
