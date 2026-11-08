// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { PubSubClient } from '../src/client.js';
import { ListTopicsInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/google/pubsub/scenarios.json', import.meta.url));
const connection = { provider: 'google-pubsub', name: 'google-pubsub' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokePubSub, ''));

async function invokePubSub(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials = {
    oauthClientId: 'synthetic-client-id', oauthClientSecret: new SecretString('synthetic-client-secret'),
    accessToken: new SecretString('test-google-pubsub-token'), refreshToken: new SecretString('synthetic-refresh-token'),
  };
  const client = new PubSubClient({ baseUrl: 'http://127.0.0.1:12345' }, new StaticCredentialProvider([[connection, credentials]]), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'google-pubsub-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'google-pubsub', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'listTopics') return client.listTopics().invoke(call, ListTopicsInputSchema.parse(scenario.input));
  throw new Error(`unknown PubSub operation ${scenario.operation}`);
}
