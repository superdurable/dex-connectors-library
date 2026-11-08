// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { KitClient } from '../src/client.js';
import { GetSubscriberInputSchema, ListSubscribersInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/kit/scenarios.json', import.meta.url));
const connection = { provider: 'kit', name: 'kit' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeKit, '/v4'));

async function invokeKit(scenario: Scenario, recorder: ExchangeRecorder) {
  const client = new KitClient({ baseUrl: 'http://127.0.0.1:12345/v4' }, new StaticCredentialProvider([[connection, { apiKey: new SecretString('test-token') }]]), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'kit-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'kit', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'listSubscribers') return client.listSubscribers().invoke(call, ListSubscribersInputSchema.parse(scenario.input));
  if (scenario.operation === 'getSubscriber') return client.getSubscriber().invoke(call, GetSubscriberInputSchema.parse(scenario.input));
  throw new Error(`unknown Kit operation ${scenario.operation}`);
}
