// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { Fit2cloudClient } from '../src/client.js';
import { ListPostsInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/fit2cloud/scenarios.json', import.meta.url));
const connection = { provider: 'fit2cloud', name: 'fit2cloud' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeFit2cloud, ''));

async function invokeFit2cloud(scenario: Scenario, recorder: ExchangeRecorder) {
  const apiKey = scenario.name.endsWith('defect') ? '' : 'pat_private-test-key';
  const credentials: Array<[typeof connection, { apiKey: SecretString }]> = [[connection, { apiKey: new SecretString(apiKey) }]];
  const client = new Fit2cloudClient({ baseUrl: 'https://halo.example.test', maxResponseBytes: scenario.name.endsWith('oversized') ? 8 : 2097152 }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'fit2cloud-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'fit2cloud', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'listPosts') return client.listPosts().invoke(call, ListPostsInputSchema.parse({ page: 0, pageSize: 0, ...scenario.input }));
  throw new Error(`unknown Fit2cloud operation ${scenario.operation}`);
}
