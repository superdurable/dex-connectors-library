// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { ShumeiClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/shumei/scenarios.json', import.meta.url));
const connection = { provider: 'shumei', name: 'shumei' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeShumei, ''));

async function invokeShumei(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'getBalance') throw new Error(`unknown Shumei operation ${scenario.operation}`);
  const isDefect = scenario.name.endsWith('-defect');
  const credentials = new StaticCredentialProvider([[connection, { accessKey: new SecretString(isDefect ? '' : 'secretTestKey') }]]);
  const client = new ShumeiClient({}, credentials, { send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)) });
  const call: Call = {
    context: { flowId: 'shumei-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'shumei', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.getBalance().invoke(call, scenario.input as unknown as Parameters<ReturnType<ShumeiClient['getBalance']>['invoke']>[1]);
}
