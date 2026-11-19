// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { OnesClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/ones/scenarios.json', import.meta.url));
const connection = { provider: 'ones', name: 'ones' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeOnes, ''));

async function invokeOnes(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials = new StaticCredentialProvider([[connection, { apiKey: new SecretString(scenario.name.endsWith('defect') ? '' : 'private-test-key') }]]);
  const client = new OnesClient({ baseUrl: "https://ones.example" }, credentials, { send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)) });
  const call: Call = {
    context: { flowId: 'ones-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'ones', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'listTeams') return client.listTeams().invoke(call, scenario.input as Parameters<ReturnType<OnesClient['listTeams']>['invoke']>[1]);
  throw new Error(`unknown Ones operation ${scenario.operation}`);
}
