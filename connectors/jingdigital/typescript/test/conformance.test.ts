// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { JINGdigitalClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/jingdigital/scenarios.json', import.meta.url));
const connection = { provider: 'jingdigital', name: 'jingdigital' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeJINGdigital));

async function invokeJINGdigital(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'listTags') throw new Error(`unknown JINGdigital operation ${scenario.operation}`);
  const client = new JINGdigitalClient({}, new StaticCredentialProvider([[connection, { appId: new SecretString('app-test'), appSecret: new SecretString('secret-test') }]]), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'jingdigital-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'jingdigital', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.listTags().invoke(call, scenario.input as { page: number; pageSize: number });
}
