// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { ZhihuClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
const scenarios = loadScenarios(new URL('../../../../conformance/connectors/zhihu/scenarios.json', import.meta.url));
const connection = { provider: 'zhihu', name: 'zhihu' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeZhihu, ''));
async function invokeZhihu(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'listQuotas') throw new Error(`unknown operation ${scenario.operation}`);
  const credentials = new StaticCredentialProvider([[connection, { accessSecret: new SecretString(scenario.name.endsWith('-defect') ? '' : 'secretTestToken') }]]);
  const client = new ZhihuClient(scenario.name === 'listQuotas-oversized' ? { maxResponseBytes: 16, ...(scenario.name.includes('oversized') && ['zadig','zentao'].includes('zhihu') ? { baseUrl: 'http://127.0.0.1:1' } : {}) } : {}, credentials, { now: () => new Date(scenario.fixedClock ?? '2026-01-02T03:04:05Z'), send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)) });
  const call: Call = { context: { flowId: 'zhihu-conformance', cancellationSignal: new AbortController().signal } as Call['context'], id: scenario.callId, connection, operation: { connectorId: 'zhihu', operationId: scenario.operation }, reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false };
  return client.listQuotas().invoke(call, scenario.input as unknown as Parameters<ReturnType<ZhihuClient['listQuotas']>['invoke']>[1]);
}
