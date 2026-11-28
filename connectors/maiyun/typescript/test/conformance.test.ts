// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { MaiyunClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/maiyun/scenarios.json', import.meta.url));
const connection = { provider: 'maiyun', name: 'maiyun' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeMaiyun, ''));

async function invokeMaiyun(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'lookupIPLocation') throw new Error(`unknown Maiyun operation ${scenario.operation}`);
  const isDefect = scenario.name.endsWith('-defect');
  const credentials = new StaticCredentialProvider([[connection, { token: new SecretString(isDefect ? '' : 'private-test-key') }]]);
  const client = new MaiyunClient({ maxResponseBytes: scenario.name.endsWith('-oversized') ? 16 : 65536 }, credentials, { send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)) });
  const call: Call = {
    context: { flowId: 'maiyun-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'maiyun', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.lookupIPLocation().invoke(call, scenario.input as unknown as Parameters<ReturnType<MaiyunClient['lookupIPLocation']>['invoke']>[1]);
}
