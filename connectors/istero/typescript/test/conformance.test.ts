// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { IsTeroClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/istero/scenarios.json', import.meta.url));
const connection = { provider: 'istero', name: 'istero' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeIsTero));

async function invokeIsTero(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'getAccountStatus') throw new Error(`unknown IsTero operation ${scenario.operation}`);
  const client = new IsTeroClient({}, new StaticCredentialProvider([[connection, { apiKey: new SecretString(scenario.name.endsWith('defect') ? '' : 'private-test-token'), isid: scenario.name.endsWith('-invalidISID') ? '060066023' : '60066023' }]]), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'istero-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'istero', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.getAccountStatus().invoke(call);
}
