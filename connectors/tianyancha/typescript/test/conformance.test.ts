// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { TianyanchaClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/tianyancha/scenarios.json', import.meta.url));
const connection = { provider: 'tianyancha', name: 'tianyancha' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeTianyancha, ''));

async function invokeTianyancha(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'getCompanyProfile') throw new Error(`unknown Tianyancha operation ${scenario.operation}`);
  const isDefect = scenario.name.endsWith('-defect');
  const credentials = new StaticCredentialProvider([[connection, { apiKey: new SecretString(isDefect ? '' : 'test-secret') }]]);
  const client = new TianyanchaClient({}, credentials, { send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)) });
  const call: Call = {
    context: { flowId: 'tianyancha-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'tianyancha', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.getCompanyProfile().invoke(call, scenario.input as unknown as Parameters<ReturnType<TianyanchaClient['getCompanyProfile']>['invoke']>[1]);
}
