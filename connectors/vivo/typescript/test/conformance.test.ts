// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { VivoClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/vivo/scenarios.json', import.meta.url));
const connection = { provider: 'vivo', name: 'vivo' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeVivo, ''));

async function invokeVivo(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'getOnlineVersion') throw new Error(`unknown Vivo operation ${scenario.operation}`);
  const credentials = new StaticCredentialProvider([[connection, { account: new SecretString('test-account'), secret: new SecretString('test-secret') }]]);
  const client = new VivoClient({}, credentials, { clock: () => 1731510000000, send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)) });
  const call: Call = {
    context: { flowId: 'vivo-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'vivo', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.getOnlineVersion().invoke(call, scenario.input as unknown as Parameters<ReturnType<VivoClient['getOnlineVersion']>['invoke']>[1]);
}
