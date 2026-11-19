// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { VikaClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/vika/scenarios.json', import.meta.url));
const connection = { provider: 'vika', name: 'vika' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeVika, ''));

async function invokeVika(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'listSpaces') throw new Error(`unknown Vika operation ${scenario.operation}`);
  const isDefect = scenario.name.endsWith('-defect');
  const credentials = new StaticCredentialProvider([[connection, { apiToken: new SecretString(isDefect ? '' : 'uskTestToken') }]]);
  const client = new VikaClient({}, credentials, { send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)) });
  const call: Call = {
    context: { flowId: 'vika-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'vika', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.listSpaces().invoke(call, scenario.input as unknown as Parameters<ReturnType<VikaClient['listSpaces']>['invoke']>[1]);
}
