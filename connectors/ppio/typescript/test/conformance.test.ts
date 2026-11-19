// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { PPIOClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/ppio/scenarios.json', import.meta.url));
const connection = { provider: 'ppio', name: 'ppio' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokePPIO, ''));

async function invokePPIO(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials = new StaticCredentialProvider([[connection, { apiKey: new SecretString(scenario.name.endsWith('defect') ? '' : 'secret-api-key') }]]);
  const client = new PPIOClient({}, credentials, { send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)) });
  const call: Call = {
    context: { flowId: 'ppio-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'ppio', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'listModels') return client.listModels().invoke(call, scenario.input as Parameters<ReturnType<PPIOClient['listModels']>['invoke']>[1]);
  throw new Error(`unknown PPIO operation ${scenario.operation}`);
}
