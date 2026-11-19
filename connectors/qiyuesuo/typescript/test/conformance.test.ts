// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { QiyuesuoClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/qiyuesuo/scenarios.json', import.meta.url));
const connection = { provider: 'qiyuesuo', name: 'qiyuesuo' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeQiyuesuo, ''));

async function invokeQiyuesuo(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'listContracts') throw new Error(`unknown Qiyuesuo operation ${scenario.operation}`);
  const isDefect = scenario.name.endsWith('-defect');
  const credentials = new StaticCredentialProvider([[connection, { appToken: new SecretString(isDefect ? '' : 'app-token'), appSecret: new SecretString('app-secret') }]]);
  const client = new QiyuesuoClient({ ...(scenario.name.endsWith('-oversized') ? { maxResponseBytes: 16 } : {}) }, credentials, { nonce: () => '254775d7-3c7c-4e9d-9880-1d4c5fb3dbd0', now: () => new Date('2024-11-13T15:00:00Z'), send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)) });
  const call: Call = {
    context: { flowId: 'qiyuesuo-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'qiyuesuo', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.listContracts().invoke(call, scenario.input as unknown as Parameters<ReturnType<QiyuesuoClient['listContracts']>['invoke']>[1]);
}
