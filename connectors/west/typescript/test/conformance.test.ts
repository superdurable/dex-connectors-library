// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { WestClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/west/scenarios.json', import.meta.url));
const connection = { provider: 'west', name: 'west' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeWest, ''));

async function invokeWest(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'listDomains') throw new Error(`unknown West operation ${scenario.operation}`);
  const credentials = new StaticCredentialProvider([[connection, { username: 'zhangsan', apiPassword: new SecretString('REDACTED_API_PASSWORD') }]]);
  const client = new WestClient(scenario.name.endsWith('-oversized') ? {maxResponseBytes: 4} : {}, credentials, { send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)), now: () => new Date(scenario.fixedClock ?? '2026-11-13T12:00:00Z') });
  const call: Call = {
    context: { flowId: 'west-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'west', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.listDomains().invoke(call, scenario.input as unknown as Parameters<ReturnType<WestClient['listDomains']>['invoke']>[1]);
}
