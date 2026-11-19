// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { Qihoo360Client } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/qihoo360/scenarios.json', import.meta.url));
const connection = { provider: 'qihoo360', name: 'qihoo360' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeQihoo360, ''));

async function invokeQihoo360(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'generateText') throw new Error(`unknown Qihoo360 operation ${scenario.operation}`);
  const isDefect = scenario.name.endsWith('-defect');
  const credentials = new StaticCredentialProvider([[connection, { apiKey: new SecretString(isDefect ? '' : 'REDACTED_QIHOO_API_KEY') }]]);
  const client = new Qihoo360Client({}, credentials, { send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)) });
  const call: Call = {
    context: { flowId: 'qihoo360-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'qihoo360', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.generateText().invoke(call, scenario.input as unknown as Parameters<ReturnType<Qihoo360Client['generateText']>['invoke']>[1]);
}
