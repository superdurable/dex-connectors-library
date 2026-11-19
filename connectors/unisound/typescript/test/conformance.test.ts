// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { UnisoundClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/unisound/scenarios.json', import.meta.url));
const connection = { provider: 'unisound', name: 'unisound' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeUnisound, ''));

async function invokeUnisound(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'listSystemVoices') throw new Error(`unknown Unisound operation ${scenario.operation}`);
  const isDefect = scenario.name.endsWith('-defect');
  const credentials = new StaticCredentialProvider([[connection, { apiKey: new SecretString(isDefect ? '' : 'test-api-key') }]]);
  const client = new UnisoundClient({}, credentials, { send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)) });
  const call: Call = {
    context: { flowId: 'unisound-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'unisound', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.listSystemVoices().invoke(call, scenario.input as unknown as Parameters<ReturnType<UnisoundClient['listSystemVoices']>['invoke']>[1]);
}
