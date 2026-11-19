// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { MeituanClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
const scenarios = loadScenarios(new URL('../../../../conformance/connectors/meituan/scenarios.json', import.meta.url));
const connection = { provider: 'meituan', name: 'meituan' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeMeituan, ''));
async function invokeMeituan(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'queryOrderStatus') throw new Error(`unknown operation ${scenario.operation}`);
  const credentials = new StaticCredentialProvider([[connection, { appKey: 'test-app-key', secret: new SecretString('private-secret') }]]);
  const client = new MeituanClient(scenario.name.endsWith('-oversized') ? { maxResponseBytes: 16 } : {}, credentials, { now: () => new Date(scenario.fixedClock ?? '2026-01-01T00:00:00Z'), send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)) });
  const call: Call = { context: { flowId: 'meituan-conformance', cancellationSignal: new AbortController().signal } as Call['context'], id: scenario.callId, connection, operation: { connectorId: 'meituan', operationId: scenario.operation }, reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false };
  return client.queryOrderStatus().invoke(call, scenario.input as unknown as Parameters<ReturnType<MeituanClient['queryOrderStatus']>['invoke']>[1]);
}
