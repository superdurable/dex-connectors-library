// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { TushareClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/tushare/scenarios.json', import.meta.url));
const connection = { provider: 'tushare', name: 'tushare' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeTushare, ''));

async function invokeTushare(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'getTradingDay') throw new Error(`unknown Tushare operation ${scenario.operation}`);
  const isDefect = scenario.name.endsWith('-defect');
  const credentials = new StaticCredentialProvider([[connection, { token: new SecretString(isDefect ? '' : 'test-secret') }]]);
  const client = new TushareClient({ ...(scenario.name.endsWith('-oversized') ? { maxResponseBytes: 16 } : {}) }, credentials, { send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)) });
  const call: Call = {
    context: { flowId: 'tushare-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'tushare', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.getTradingDay().invoke(call, scenario.input as unknown as Parameters<ReturnType<TushareClient['getTradingDay']>['invoke']>[1]);
}
