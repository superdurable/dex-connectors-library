// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { SubmailClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/submail/scenarios.json', import.meta.url));
const connection = { provider: 'submail', name: 'submail' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeSubmail, ''));

async function invokeSubmail(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'getSMSBalance') throw new Error(`unknown Submail operation ${scenario.operation}`);
  const isDefect = scenario.name.endsWith('-defect');
  const credentials = new StaticCredentialProvider([[connection, { appId: isDefect ? '' : 'test-app-id', appKey: new SecretString('test-app-key') }]]);
  const client = new SubmailClient({}, credentials, { send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)) });
  const call: Call = {
    context: { flowId: 'submail-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'submail', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.getSMSBalance().invoke(call, scenario.input as unknown as Parameters<ReturnType<SubmailClient['getSMSBalance']>['invoke']>[1]);
}
