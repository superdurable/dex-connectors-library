// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { ZenTaoClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
const scenarios = loadScenarios(new URL('../../../../conformance/connectors/zentao/scenarios.json', import.meta.url));
const connection = { provider: 'zentao', name: 'zentao' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeZenTao, ''));
async function invokeZenTao(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'listProducts') throw new Error(`unknown operation ${scenario.operation}`);
  const credentials = new StaticCredentialProvider([[connection, { token: new SecretString(scenario.name.endsWith('-defect') ? '' : 'secretToken') }]]);
  const client = new ZenTaoClient(scenario.name === 'listProducts-oversized' ? { baseUrl: 'http://127.0.0.1:1', maxResponseBytes: 16 } : { baseUrl: 'http://127.0.0.1:1' }, credentials, { now: () => new Date(scenario.fixedClock ?? '2026-01-02T03:04:05Z'), send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)) });
  const call: Call = { context: { flowId: 'zentao-conformance', cancellationSignal: new AbortController().signal } as Call['context'], id: scenario.callId, connection, operation: { connectorId: 'zentao', operationId: scenario.operation }, reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false };
  return client.listProducts().invoke(call, scenario.input as unknown as Parameters<ReturnType<ZenTaoClient['listProducts']>['invoke']>[1]);
}
