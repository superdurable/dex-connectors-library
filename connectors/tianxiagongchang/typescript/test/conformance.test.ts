// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { TianxiaGongchangClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/tianxiagongchang/scenarios.json', import.meta.url));
const connection = { provider: 'tianxiagongchang', name: 'tianxiagongchang' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeTianxiaGongchang, ''));

async function invokeTianxiaGongchang(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'getAccountStatus') throw new Error(`unknown TianxiaGongchang operation ${scenario.operation}`);
  const isDefect = scenario.name.endsWith('-defect');
  const credentials = new StaticCredentialProvider([[connection, { apiKey: new SecretString(isDefect ? '' : 'sk-tx-live-test-secret') }]]);
  const client = new TianxiaGongchangClient({}, credentials, { send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)) });
  const call: Call = {
    context: { flowId: 'tianxiagongchang-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'tianxiagongchang', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.getAccountStatus().invoke(call, scenario.input as unknown as Parameters<ReturnType<TianxiaGongchangClient['getAccountStatus']>['invoke']>[1]);
}
