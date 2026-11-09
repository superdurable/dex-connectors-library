// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { LucidClient } from '../src/client.js';
import { ListDocumentsInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/lucid/scenarios.json', import.meta.url));
const connection = { provider: 'lucid', name: 'lucid' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeLucid, '/v1'));

async function invokeLucid(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { apiKey: SecretString }]> = [[connection, { apiKey: new SecretString('sample-lucid-key') }]];
  const client = new LucidClient({ baseUrl: 'http://127.0.0.1:12345/v1' }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'lucid-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'lucid', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'listDocuments') return client.listDocuments().invoke(call, ListDocumentsInputSchema.parse(scenario.input));
  throw new Error(`unknown Lucid operation ${scenario.operation}`);
}
