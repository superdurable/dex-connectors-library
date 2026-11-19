// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { TingyunClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/tingyun/scenarios.json', import.meta.url));
const connection = { provider: 'tingyun', name: 'tingyun' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeTingyun, ''));

async function invokeTingyun(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'listApplications') throw new Error(`unknown Tingyun operation ${scenario.operation}`);
  const isDefect = scenario.name.endsWith('-defect');
  const credentials = new StaticCredentialProvider([[connection, { accessToken: new SecretString(isDefect ? '' : 'access-secret') }]]);
  const client = new TingyunClient({ apiHost: 'https://wukong2.tingyun.com/' }, credentials, { send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)) });
  const call: Call = {
    context: { flowId: 'tingyun-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'tingyun', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.listApplications().invoke(call, scenario.input as unknown as Parameters<ReturnType<TingyunClient['listApplications']>['invoke']>[1]);
}
