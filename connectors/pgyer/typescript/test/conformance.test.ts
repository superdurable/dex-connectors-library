// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { PgyerClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/pgyer/scenarios.json', import.meta.url));
const connection = { provider: 'pgyer', name: 'pgyer' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokePgyer, ''));

async function invokePgyer(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials = new StaticCredentialProvider([[connection, { apiKey: new SecretString('secretKey') }]]);
  const client = new PgyerClient({}, credentials, { send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)) });
  const call: Call = {
    context: { flowId: 'pgyer-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'pgyer', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'listUploadedApps') return client.listUploadedApps().invoke(call, scenario.input as unknown as Parameters<ReturnType<PgyerClient['listUploadedApps']>['invoke']>[1]);
  throw new Error(`unknown Pgyer operation ${scenario.operation}`);
}
