// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { OneboundClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
const scenarios = loadScenarios(new URL('../../../../conformance/connectors/onebound/scenarios.json', import.meta.url));
const connection = { provider: 'onebound', name: 'onebound' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeOnebound, ''));
async function invokeOnebound(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'searchProducts') throw new Error(`unknown operation ${scenario.operation}`);
  const credentials = new StaticCredentialProvider([[connection, { apiKey: new SecretString('testKey'), apiSecret: new SecretString('testSecret') }]]);
  const client = new OneboundClient(scenario.name.endsWith('-oversized') ? { maxResponseBytes: 16 } : {}, credentials, { now: () => new Date(scenario.fixedClock ?? '2026-01-01T00:00:00Z'), send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)) });
  const call: Call = { context: { flowId: 'onebound-conformance', cancellationSignal: new AbortController().signal } as Call['context'], id: scenario.callId, connection, operation: { connectorId: 'onebound', operationId: scenario.operation }, reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false };
  return client.searchProducts().invoke(call, scenario.input as unknown as Parameters<ReturnType<OneboundClient['searchProducts']>['invoke']>[1]);
}
