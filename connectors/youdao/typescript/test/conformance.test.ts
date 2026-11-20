// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { YoudaoClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/youdao/scenarios.json', import.meta.url));
const connection = { provider: 'youdao', name: 'youdao' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeYoudao, ''));

async function invokeYoudao(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'translateText') throw new Error(`unknown Youdao operation ${scenario.operation}`);
  const credentials = new StaticCredentialProvider([[connection, { appKey: 'test-app', appSecret: new SecretString('REDACTED_APP_SECRET') }]]);
  const client = new YoudaoClient(scenario.name.endsWith('-oversized') ? {maxResponseBytes: 5} : {}, credentials, { send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)), now: () => new Date(scenario.fixedClock ?? '2026-11-13T12:00:00Z'), salt: () => new Uint8Array(16).fill(0x42) });
  const call: Call = {
    context: { flowId: 'youdao-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'youdao', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.translateText().invoke(call, scenario.input as unknown as Parameters<ReturnType<YoudaoClient['translateText']>['invoke']>[1]);
}
