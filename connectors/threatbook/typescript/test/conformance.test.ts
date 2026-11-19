// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { ThreatBookClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/threatbook/scenarios.json', import.meta.url));
const connection = { provider: 'threatbook', name: 'threatbook' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeThreatBook, ''));

async function invokeThreatBook(scenario: Scenario, recorder: ExchangeRecorder) {
  const apiKey = scenario.name.endsWith('defect') ? '' : 'test-secret';
  const credentials: Array<[typeof connection, { apiKey: SecretString }]> = [[connection, { apiKey: new SecretString(apiKey) }]];
  const client = new ThreatBookClient({ maxResponseBytes: scenario.name.endsWith('oversized') ? 8 : 1048576 }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'threatbook-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'threatbook', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'checkIPReputation') return client.checkIPReputation().invoke(call, scenario.input as { ipAddress: string });
  throw new Error(`unknown ThreatBook operation ${scenario.operation}`);
}
