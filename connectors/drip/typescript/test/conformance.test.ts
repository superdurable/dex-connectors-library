// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { DripClient } from '../src/client.js';
import { GetSubscriberInputSchema, ListAccountsInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/drip/scenarios.json', import.meta.url));
const connection = { provider: 'drip', name: 'drip' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeDrip, '/v2'));

async function invokeDrip(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { apiKey: SecretString }]> = scenario.name === 'listAccounts-defect' ? [] : [[connection, { apiKey: new SecretString('test-token') }]];
  const client = new DripClient({ baseUrl: 'http://127.0.0.1:12345/v2' }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'drip-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'drip', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'listAccounts') return client.listAccounts().invoke(call, ListAccountsInputSchema.parse(scenario.input));
  if (scenario.operation === 'getSubscriber') return client.getSubscriber().invoke(call, GetSubscriberInputSchema.parse(scenario.input));
  throw new Error(`unknown Drip operation ${scenario.operation}`);
}
