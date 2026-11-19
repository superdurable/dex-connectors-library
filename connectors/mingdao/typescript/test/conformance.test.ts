// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { MingdaoClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/mingdao/scenarios.json', import.meta.url));
const connection = { provider: 'mingdao', name: 'mingdao' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeMingdao, ''));

async function invokeMingdao(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'listWorksheets') throw new Error(`unknown Mingdao operation ${scenario.operation}`);
  const isDefect = scenario.name.endsWith('-defect');
  const credentials = new StaticCredentialProvider([[connection, { appKey: new SecretString(isDefect ? '' : 'app-secret'), sign: new SecretString(isDefect ? '' : 'sign-secret') }]]);
  const client = new MingdaoClient({}, credentials, { send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)) });
  const call: Call = {
    context: { flowId: 'mingdao-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'mingdao', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.listWorksheets().invoke(call, scenario.input as unknown as Parameters<ReturnType<MingdaoClient['listWorksheets']>['invoke']>[1]);
}
