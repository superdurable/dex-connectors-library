// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { OpenCSGClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/opencsg/scenarios.json', import.meta.url));
const connection = { provider: 'opencsg', name: 'opencsg' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeOpenCSG, ''));

async function invokeOpenCSG(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials = new StaticCredentialProvider([[connection, { apiKey: new SecretString(scenario.name.endsWith('defect') ? '' : 'test-secret') }]]);
  const client = new OpenCSGClient({}, credentials, { send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)) });
  const call: Call = {
    context: { flowId: 'opencsg-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'opencsg', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'listGatewayModels') return client.listGatewayModels().invoke(call, scenario.input as Parameters<ReturnType<OpenCSGClient['listGatewayModels']>['invoke']>[1]);
  throw new Error(`unknown OpenCSG operation ${scenario.operation}`);
}
