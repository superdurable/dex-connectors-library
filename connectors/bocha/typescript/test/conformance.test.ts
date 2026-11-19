// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { BochaClient } from '../src/client.js';
import { SearchWebInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/bocha/scenarios.json', import.meta.url));
const connection = { provider: 'bocha', name: 'bocha' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeBocha, ''));

async function invokeBocha(scenario: Scenario, recorder: ExchangeRecorder) {
  const apiKey = scenario.name.endsWith('defect') ? '' : 'test-bocha-key';
  const credentials: Array<[typeof connection, { apiKey: SecretString }]> = [[connection, { apiKey: new SecretString(apiKey) }]];
  const client = new BochaClient({ maxResponseBytes: scenario.name.endsWith('oversized') ? 8 : 1_048_576 }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'bocha-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'bocha', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'searchWeb') return client.searchWeb().invoke(call, SearchWebInputSchema.parse(scenario.input));
  throw new Error('unknown Bocha operation ' + scenario.operation);
}
