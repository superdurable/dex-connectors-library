// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { Chukou1Client } from '../src/client.js';
import { ListWarehousesInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/chukou1/scenarios.json', import.meta.url));
const connection = { provider: 'chukou1', name: 'chukou1' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeChukou1, ''));

async function invokeChukou1(scenario: Scenario, recorder: ExchangeRecorder) {
  const accessToken = scenario.name === 'listWarehouses-missing-credentials' ? '' : 'secretTestToken';
  const client = new Chukou1Client({ baseUrl: 'http://127.0.0.1:12345', ...(scenario.name === 'listWarehouses-oversize' ? { maxResponseBytes: 4 } : {}) },
    new StaticCredentialProvider([[connection, { accessToken: new SecretString(accessToken) }]]),
    { send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)) });
  const call: Call = {
    context: { flowId: 'chukou1-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'chukou1', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'listWarehouses') return client.listWarehouses().invoke(call, ListWarehousesInputSchema.parse(scenario.input));
  throw new Error(`unknown Chukou1 operation ${scenario.operation}`);
}
