// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { SiliconFlowClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/siliconflow/scenarios.json', import.meta.url));
const connection = { provider: 'siliconflow', name: 'siliconflow' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeSiliconFlow, ''));

async function invokeSiliconFlow(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'listModels') throw new Error(`unknown SiliconFlow operation ${scenario.operation}`);
  const isDefect = scenario.name.endsWith('-defect');
  const credentials = new StaticCredentialProvider([[connection, { apiKey: new SecretString(isDefect ? '' : 'test-siliconflow-key-123') }]]);
  const config = scenario.name === 'listModels-oversized' ? { maxResponseBytes: 16 } : {};
  const client = new SiliconFlowClient(config, credentials, { send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)) });
  const call: Call = {
    context: { flowId: 'siliconflow-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'siliconflow', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.listModels().invoke(call, scenario.input as unknown as Parameters<ReturnType<SiliconFlowClient['listModels']>['invoke']>[1]);
}
