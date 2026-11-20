// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { ZEGOClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, SyntheticRandomSource, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
const scenarios = loadScenarios(new URL('../../../../conformance/connectors/zego/scenarios.json', import.meta.url));
const connection = { provider: 'zego', name: 'zego' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeZEGO, ''));
async function invokeZEGO(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'getRoomUserCount') throw new Error(`unknown operation ${scenario.operation}`);
  const credentials = new StaticCredentialProvider([[connection, { appId: '12345', serverSecret: new SecretString('test-secret') }]]);
  const random = new SyntheticRandomSource(scenario.randomSeed ?? '');
  const client = new ZEGOClient(scenario.name === 'getRoomUserCount-oversized' ? { maxResponseBytes: 16 } : {}, credentials, { now: () => new Date(scenario.fixedClock ?? '2026-01-02T03:04:05Z'), random: count => random.read(count), send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)) });
  const call: Call = { context: { flowId: 'zego-conformance', cancellationSignal: new AbortController().signal } as Call['context'], id: scenario.callId, connection, operation: { connectorId: 'zego', operationId: scenario.operation }, reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false };
  return client.getRoomUserCount().invoke(call, scenario.input as unknown as Parameters<ReturnType<ZEGOClient['getRoomUserCount']>['invoke']>[1]);
}
