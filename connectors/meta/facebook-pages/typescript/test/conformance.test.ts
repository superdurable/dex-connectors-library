// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { FacebookPagesClient } from '../src/client.js';
import { GetPostInputSchema, ListPostsInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/meta/facebook-pages/scenarios.json', import.meta.url))
  .filter(scenario => scenario.operation === 'listPosts' || scenario.operation === 'getPost');
const connection = { provider: 'facebook-pages', name: 'facebook-pages' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeFacebookPages, '/v25.0'));

async function invokeFacebookPages(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { pageAccessToken: SecretString }]> = [[connection, { pageAccessToken: new SecretString('test-page-token') }]];
  const client = new FacebookPagesClient({ baseUrl: 'http://127.0.0.1:12345/v25.0' }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'facebook-pages-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'facebook-pages', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'listPosts') return client.listPosts().invoke(call, ListPostsInputSchema.parse(scenario.input));
  if (scenario.operation === 'getPost') return client.getPost().invoke(call, GetPostInputSchema.parse(scenario.input));
  throw new Error(`unknown Facebook Pages operation ${scenario.operation}`);
}
