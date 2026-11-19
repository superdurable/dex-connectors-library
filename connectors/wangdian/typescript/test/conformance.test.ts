// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { WangdianClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/wangdian/scenarios.json', import.meta.url));
const connection = { provider: 'wangdian', name: 'wangdian' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeWangdian, ''));

async function invokeWangdian(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'listShops') throw new Error(`unknown Wangdian operation ${scenario.operation}`);
  const isDefect = scenario.name.endsWith('-defect');
  const credentials = new StaticCredentialProvider([[connection, { sid: new SecretString(isDefect ? '' : 'test2'), appKey: new SecretString('test2-xx'), appSecret: new SecretString('12345') }]]);
  const client = new WangdianClient({ ...(scenario.name.endsWith('-oversized') ? { maxResponseBytes: 16 } : {}) }, credentials, { now: () => new Date('2024-11-13T15:00:00Z'), send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)) });
  const call: Call = {
    context: { flowId: 'wangdian-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'wangdian', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.listShops().invoke(call, scenario.input as unknown as Parameters<ReturnType<WangdianClient['listShops']>['invoke']>[1]);
}
