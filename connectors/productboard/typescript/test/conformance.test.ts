// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { ProductboardClient } from '../src/client.js';
import { ListFeaturesInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/productboard/scenarios.json', import.meta.url));
const connection = { provider: 'productboard', name: 'productboard' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeProductboard, '/v2'));

async function invokeProductboard(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { apiToken: SecretString }]> = [[connection, { apiToken: new SecretString('sample-productboard-token') }]];
  const client = new ProductboardClient({ baseUrl: 'http://127.0.0.1:12345/v2' }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'productboard-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'productboard', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'listFeatures') return client.listFeatures().invoke(call, ListFeaturesInputSchema.parse(scenario.input));
  throw new Error(`unknown Productboard operation ${scenario.operation}`);
}
