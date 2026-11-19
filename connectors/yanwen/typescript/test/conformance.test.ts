// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { YanwenClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/yanwen/scenarios.json', import.meta.url));
const connection = { provider: 'yanwen', name: 'yanwen' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeYanwen, ''));

async function invokeYanwen(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'listWarehouses') throw new Error(`unknown Yanwen operation ${scenario.operation}`);
  const isDefect = scenario.name.endsWith('-defect');
  const credentials = new StaticCredentialProvider([[connection, { userId: isDefect ? '' : 'customer-1', apiToken: new SecretString('REDACTED_API_TOKEN') }]]);
  const client = new YanwenClient(scenario.name.endsWith('-oversized') ? {maxResponseBytes: 8} : {}, credentials, { send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)), now: () => new Date(scenario.fixedClock ?? '2026-11-13T12:00:00Z') });
  const call: Call = {
    context: { flowId: 'yanwen-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'yanwen', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.listWarehouses().invoke(call);
}
