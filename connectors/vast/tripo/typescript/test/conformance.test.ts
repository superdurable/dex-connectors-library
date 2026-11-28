// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { TripoClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/vast/tripo/scenarios.json', import.meta.url));
const connection = { provider: 'tripo', name: 'tripo' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeTripo, ''));

async function invokeTripo(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'getAccountBalance') throw new Error(`unknown Tripo operation ${scenario.operation}`);
  const isDefect = scenario.name.endsWith('-defect');
  const credentials = new StaticCredentialProvider([[connection, { apiKey: new SecretString(isDefect ? '' : 'test-api-key') }]]);
  const client = new TripoClient(scenario.name.endsWith('-oversize') ? { maxResponseBytes: 8 } : {}, credentials, { send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)) });
  const call: Call = {
    context: { flowId: 'tripo-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'tripo', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.getAccountBalance().invoke(call, scenario.input as unknown as Parameters<ReturnType<TripoClient['getAccountBalance']>['invoke']>[1]);
}
