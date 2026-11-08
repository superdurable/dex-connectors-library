// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { ChargebeeClient } from '../src/client.js';
import { GetSubscriptionInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/chargebee/scenarios.json', import.meta.url));
const connection = { provider: 'chargebee', name: 'chargebee' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeChargebee, '/api'));

async function invokeChargebee(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { apiKey: SecretString }]> = [[connection, { apiKey: new SecretString('sample-chargebee-api-token') }]];
  const client = new ChargebeeClient({ baseUrl: 'http://127.0.0.1:12345/api' }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'chargebee-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'chargebee', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getSubscription') return client.getSubscription().invoke(call, GetSubscriptionInputSchema.parse(scenario.input));
  throw new Error(`unknown Chargebee operation ${scenario.operation}`);
}
