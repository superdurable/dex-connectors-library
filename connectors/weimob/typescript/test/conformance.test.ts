// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { WeimobClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/weimob/scenarios.json', import.meta.url));
const connection = { provider: 'weimob', name: 'weimob' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeWeimob, ''));

async function invokeWeimob(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'getStoreBasic') throw new Error(`unknown Weimob operation ${scenario.operation}`);
  const isDefect = scenario.name.endsWith('-defect');
  const credentials = new StaticCredentialProvider([[connection, { clientID: 'example-client-id', clientSecret: new SecretString('REDACTED_CLIENT_SECRET'), shopID: isDefect ? 'bad-shop' : '400019' }]]);
  const client = new WeimobClient(scenario.name.endsWith('-oversized') ? {maxResponseBytes: 5} : {}, credentials, { send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)), now: () => new Date(scenario.fixedClock ?? '2026-11-12T10:00:00Z') });
  const call: Call = {
    context: { flowId: 'weimob-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'weimob', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.getStoreBasic().invoke(call);
}
