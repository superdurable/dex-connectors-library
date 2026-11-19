// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { YunpianClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
const scenarios = loadScenarios(new URL('../../../../conformance/connectors/yunpian/scenarios.json', import.meta.url));
const connection = { provider: 'yunpian', name: 'yunpian' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeYunpian, ''));
async function invokeYunpian(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'getAccountBalance') throw new Error(`unknown operation ${scenario.operation}`);
  const credentials = new StaticCredentialProvider([[connection, { apiKey: new SecretString(scenario.name.endsWith('-defect') ? '' : 'sample-api-key') }]]);
  const client = new YunpianClient(scenario.name === 'getAccountBalance-oversized' ? { maxResponseBytes: 16, ...(scenario.name.includes('oversized') && ['zadig','zentao'].includes('yunpian') ? { baseUrl: 'http://127.0.0.1:1' } : {}) } : {}, credentials, { now: () => new Date(scenario.fixedClock ?? '2026-01-02T03:04:05Z'), send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)) });
  const call: Call = { context: { flowId: 'yunpian-conformance', cancellationSignal: new AbortController().signal } as Call['context'], id: scenario.callId, connection, operation: { connectorId: 'yunpian', operationId: scenario.operation }, reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false };
  return client.getAccountBalance().invoke(call, scenario.input as unknown as Parameters<ReturnType<YunpianClient['getAccountBalance']>['invoke']>[1]);
}
