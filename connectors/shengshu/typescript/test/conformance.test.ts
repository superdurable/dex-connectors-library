// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { ShengshuClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/shengshu/scenarios.json', import.meta.url));
const connection = { provider: 'shengshu', name: 'shengshu' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeShengshu, ''));

async function invokeShengshu(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials = new StaticCredentialProvider([[connection, { apiKey: new SecretString('secret-api-key') }]]);
  const client = new ShengshuClient({}, credentials, { send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)) });
  const call: Call = {
    context: { flowId: 'shengshu-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'shengshu', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getTaskStatus') return client.getTaskStatus().invoke(call, scenario.input as unknown as Parameters<ReturnType<ShengshuClient['getTaskStatus']>['invoke']>[1]);
  throw new Error(`unknown Vidu operation ${scenario.operation}`);
}
