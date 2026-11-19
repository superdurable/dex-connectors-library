// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { ShoplazzaClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/shoplazza/scenarios.json', import.meta.url));
const connection = { provider: 'shoplazza', name: 'shoplazza' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeShoplazza, ''));

async function invokeShoplazza(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'getShop') throw new Error(`unknown Shoplazza operation ${scenario.operation}`);
  const isDefect = scenario.name.endsWith('-defect');
  const credentials = new StaticCredentialProvider([[connection, { accessToken: new SecretString(isDefect ? '' : 'test-shoplazza-token') }]]);
  const client = new ShoplazzaClient({ shopDomain: 'example.myshoplaza.com' }, credentials, { send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)) });
  const call: Call = {
    context: { flowId: 'shoplazza-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'shoplazza', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.getShop().invoke(call, scenario.input as unknown as Parameters<ReturnType<ShoplazzaClient['getShop']>['invoke']>[1]);
}
