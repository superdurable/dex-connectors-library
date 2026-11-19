// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { TokenFactoryClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/tokenfactory/scenarios.json', import.meta.url));
const connection = { provider: 'tokenfactory', name: 'tokenfactory' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeTokenFactory, ''));

async function invokeTokenFactory(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'getBalance') throw new Error(`unknown TokenFactory operation ${scenario.operation}`);
  const isDefect = scenario.name.endsWith('-defect');
  const credentials = new StaticCredentialProvider([[connection, { apiKey: new SecretString(isDefect ? '' : 'sk-test-secret') }]]);
  const client = new TokenFactoryClient({}, credentials, { send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)) });
  const call: Call = {
    context: { flowId: 'tokenfactory-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'tokenfactory', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.getBalance().invoke(call, scenario.input as unknown as Parameters<ReturnType<TokenFactoryClient['getBalance']>['invoke']>[1]);
}
