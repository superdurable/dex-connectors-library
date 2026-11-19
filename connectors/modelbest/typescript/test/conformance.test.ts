// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { ModelBestClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/modelbest/scenarios.json', import.meta.url));
const connection = { provider: 'modelbest', name: 'modelbest' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeModelBest, ''));

async function invokeModelBest(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'listModels') throw new Error(`unknown ModelBest operation ${scenario.operation}`);
  const isDefect = scenario.name.endsWith('-defect');
  const credentials = new StaticCredentialProvider([[connection, { apiKey: new SecretString(isDefect ? '' : 'test-secret') }]]);
  const client = new ModelBestClient({}, credentials, { send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)) });
  const call: Call = {
    context: { flowId: 'modelbest-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'modelbest', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.listModels().invoke(call, scenario.input as unknown as Parameters<ReturnType<ModelBestClient['listModels']>['invoke']>[1]);
}
