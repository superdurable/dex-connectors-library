// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { InstagramBusinessClient } from '../src/client.js';
import { ListMediaInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/meta/instagram-business/scenarios.json', import.meta.url));
const connection = { provider: 'instagram-business', name: 'instagram-business' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeInstagramBusiness, '/v25.0'));

async function invokeInstagramBusiness(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { userAccessToken: SecretString }]> = [[connection, { userAccessToken: new SecretString('test-instagram-token') }]];
  const client = new InstagramBusinessClient({ baseUrl: 'http://127.0.0.1:12345/v25.0' }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'instagram-business-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'instagram-business', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'listMedia') return client.listMedia().invoke(call, ListMediaInputSchema.parse(scenario.input));
  throw new Error(`unknown Instagram Business operation ${scenario.operation}`);
}
