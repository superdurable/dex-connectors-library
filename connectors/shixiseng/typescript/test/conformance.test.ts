// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { ShixisengClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
const scenarios = loadScenarios(new URL('../../../../conformance/connectors/shixiseng/scenarios.json', import.meta.url));
const connection = { provider: 'shixiseng', name: 'shixiseng' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeShixiseng, ''));
async function invokeShixiseng(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'listIndustries') throw new Error(`unknown operation ${scenario.operation}`);
  const invalid = scenario.name.endsWith('-defect');
  const credentials = new StaticCredentialProvider([[connection, { appID: new SecretString(invalid ? '' : 'merchant-id'), appSecret: new SecretString(invalid ? '' : 'merchant-secret') }]]);
  const client = new ShixisengClient({}, credentials, { now: () => new Date(1731510000 * 1000), send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)) });
  const call: Call = { context: { flowId: 'shixiseng-conformance', cancellationSignal: new AbortController().signal } as Call['context'], id: scenario.callId, connection, operation: { connectorId: 'shixiseng', operationId: scenario.operation }, reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false };
  return client.listIndustries().invoke(call, scenario.input as Parameters<ReturnType<ShixisengClient['listIndustries']>['invoke']>[1]);
}
