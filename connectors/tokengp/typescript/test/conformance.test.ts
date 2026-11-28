// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { TokenGPClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/tokengp/scenarios.json', import.meta.url));
const connection = { provider: 'tokengp', name: 'tokengp' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeTokenGP, ''));

async function invokeTokenGP(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'listModels') throw new Error(`unknown TokenGP operation ${scenario.operation}`);
  const isDefect = scenario.name.endsWith('-defect');
  const credentials = new StaticCredentialProvider([[connection, { apiKey: new SecretString(isDefect ? '' : 'sk-test-secret') }]]);
  const config = scenario.name === 'listModels-oversized' ? { maxResponseBytes: 16 } : {};
  const client = new TokenGPClient(config, credentials, { send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)) });
  const call: Call = {
    context: { flowId: 'tokengp-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'tokengp', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.listModels().invoke(call, scenario.input as unknown as Parameters<ReturnType<TokenGPClient['listModels']>['invoke']>[1]);
}
