// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { TextinClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/textin/scenarios.json', import.meta.url));
const connection = { provider: 'textin', name: 'textin' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeTextin, ''));

async function invokeTextin(scenario: Scenario, recorder: ExchangeRecorder) {
  const appId = scenario.name.endsWith('defect') ? '' : 'test-app-id';
  const credentials: Array<[typeof connection, { appId: string; secretCode: SecretString }]> = [[connection, { appId, secretCode: new SecretString('secret-test-code') }]];
  const client = new TextinClient({ maxResponseBytes: scenario.name.endsWith('oversized') ? 8 : 1048576 }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'textin-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'textin', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getAccountBalance') return client.getAccountBalance().invoke(call);
  throw new Error(`unknown Textin operation ${scenario.operation}`);
}
