// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { OnenetClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
const scenarios = loadScenarios(new URL('../../../../conformance/connectors/onenet/scenarios.json', import.meta.url));
const connection = { provider: 'onenet', name: 'onenet' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeOnenet, ''));
async function invokeOnenet(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'queryDeviceDetail') throw new Error(`unknown operation ${scenario.operation}`);
  const credentials = new StaticCredentialProvider([[connection, { userId: '130037', accessKey: new SecretString('dGVzdC1hY2Nlc3Mta2V5'), productId: 'demo123' }]]);
  const client = new OnenetClient(scenario.name.endsWith('-oversized') ? { maxResponseBytes: 5 } : {}, credentials, { now: () => new Date(scenario.fixedClock ?? '2026-01-01T00:00:00Z'), send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)) });
  const call: Call = { context: { flowId: 'onenet-conformance', cancellationSignal: new AbortController().signal } as Call['context'], id: scenario.callId, connection, operation: { connectorId: 'onenet', operationId: scenario.operation }, reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false };
  return client.queryDeviceDetail().invoke(call, scenario.input as unknown as Parameters<ReturnType<OnenetClient['queryDeviceDetail']>['invoke']>[1]);
}
