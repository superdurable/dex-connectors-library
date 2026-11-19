// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { JushuitanClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
const scenarios = loadScenarios(new URL('../../../../conformance/connectors/jushuitan/scenarios.json', import.meta.url));
const connection = { provider: 'jushuitan', name: 'jushuitan' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeJushuitan, ''));
async function invokeJushuitan(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'listShops') throw new Error(`unknown operation ${scenario.operation}`);
  const credentials = new StaticCredentialProvider([[connection, { appKey: new SecretString('key-123'), appSecret: new SecretString('secret-123'), accessToken: new SecretString('token-123') }]]);
  const client = new JushuitanClient(scenario.name.endsWith('-oversized') ? { maxResponseBytes: 16 } : {}, credentials, { now: () => new Date(scenario.fixedClock ?? '2026-01-01T00:00:00Z'), send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)) });
  const call: Call = { context: { flowId: 'jushuitan-conformance', cancellationSignal: new AbortController().signal } as Call['context'], id: scenario.callId, connection, operation: { connectorId: 'jushuitan', operationId: scenario.operation }, reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false };
  return client.listShops().invoke(call, scenario.input as unknown as Parameters<ReturnType<JushuitanClient['listShops']>['invoke']>[1]);
}
