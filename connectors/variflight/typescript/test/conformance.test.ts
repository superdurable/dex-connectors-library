// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { VariFlightClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/variflight/scenarios.json', import.meta.url));
const connection = { provider: 'variflight', name: 'variflight' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeVariFlight, ''));

async function invokeVariFlight(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'getFlightStatus') throw new Error(`unknown VariFlight operation ${scenario.operation}`);
  const credentials = new StaticCredentialProvider([[connection, { appID: new SecretString('test-app'), appSecurity: new SecretString('test-secret') }]]);
  const client = new VariFlightClient({}, credentials, { send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)) });
  const call: Call = {
    context: { flowId: 'variflight-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'variflight', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.getFlightStatus().invoke(call, scenario.input as unknown as Parameters<ReturnType<VariFlightClient['getFlightStatus']>['invoke']>[1]);
}
