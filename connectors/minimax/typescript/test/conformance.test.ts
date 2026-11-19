// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { MiniMaxClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/minimax/scenarios.json', import.meta.url));
const connection = { provider: 'minimax', name: 'minimax' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeMiniMax, ''));

async function invokeMiniMax(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'getTokenPlanRemains') throw new Error(`unknown MiniMax operation ${scenario.operation}`);
  const isDefect = scenario.name.endsWith('-defect');
  const credentials = new StaticCredentialProvider([[connection, { apiKey: new SecretString(isDefect ? '' : 'sk-cp-test-minimax-key') }]]);
  const client = new MiniMaxClient({}, credentials, { send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)) });
  const call: Call = {
    context: { flowId: 'minimax-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'minimax', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.getTokenPlanRemains().invoke(call, scenario.input as unknown as Parameters<ReturnType<MiniMaxClient['getTokenPlanRemains']>['invoke']>[1]);
}
