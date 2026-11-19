// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { Doc2xClient } from '../src/client.js';
import { GetParseStatusInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/doc2x/scenarios.json', import.meta.url));
const connection = { provider: 'doc2x', name: 'doc2x' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeDoc2x, ''));

async function invokeDoc2x(scenario: Scenario, recorder: ExchangeRecorder) {
  const apiKey = scenario.name.endsWith('missing-key') ? '' : 'test-secret';
  const credentials: Array<[typeof connection, { apiKey: SecretString }]> = [[connection, { apiKey: new SecretString(apiKey) }]];
  const client = new Doc2xClient({ maxResponseBytes: scenario.name.endsWith('oversize') ? 8 : 1048576 }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'doc2x-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'doc2x', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getParseStatus') return client.getParseStatus().invoke(call, GetParseStatusInputSchema.parse(scenario.input));
  throw new Error(`unknown Doc2x operation ${scenario.operation}`);
}
