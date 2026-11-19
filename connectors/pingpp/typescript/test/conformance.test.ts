// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { PingPPClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/pingpp/scenarios.json', import.meta.url));
const connection = { provider: 'pingpp', name: 'pingpp' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokePingPP, ''));

async function invokePingPP(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials = new StaticCredentialProvider([[connection, { apiKey: new SecretString('sk_test_sampleSecret') }]]);
  const client = new PingPPClient({}, credentials, { send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)) });
  const call: Call = {
    context: { flowId: 'pingpp-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'pingpp', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getChargeStatus') return client.getChargeStatus().invoke(call, scenario.input as unknown as Parameters<ReturnType<PingPPClient['getChargeStatus']>['invoke']>[1]);
  throw new Error(`unknown PingPP operation ${scenario.operation}`);
}
