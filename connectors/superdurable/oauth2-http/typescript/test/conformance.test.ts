// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, StaticCredentialProvider, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, invokeProductionMutation, loadScenarios, replay, scenarioCall, type HeartbeatContext, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';
import { OAuth2HTTPClient } from '../src/client.js';
import { PostNonIdempotentInputSchema, SendRequestInputSchema, type SendRequestOutput } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/superdurable/oauth2-http/scenarios.json', import.meta.url));
const connection = { provider: 'oauth2-http', name: 'protected-api' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeOAuth2HTTP, '/v1'));

function productionMutation<Input>(operation: Mutation<Input, SendRequestOutput>, call: Call, input: Input, key: string) {
  return invokeProductionMutation(operation, key,
    recording => runMutation(recording as Mutation<Input, SendRequestOutput>, call, input), error => error instanceof RetryError);
}

async function invokeOAuth2HTTP(scenario: Scenario, recorder: ExchangeRecorder, context: HeartbeatContext) {
  const send = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const client = new OAuth2HTTPClient({
    authorizationEndpoint: 'http://127.0.0.1:12345/auth', tokenEndpoint: 'http://127.0.0.1:12345/token',
    scopes: ['profile'], apiBaseUrl: 'http://127.0.0.1:12345/v1',
  }, new StaticCredentialProvider([[connection, { oauthClientId: 'dex-test-client', oauthClientSecret: new SecretString('SENTINEL-CLIENT-SECRET'),
    accessToken: new SecretString('conformance-access-token'), refreshToken: new SecretString('') }]]), {
    send, now: () => new Date(scenario.fixedClock ?? '1970-01-01T00:00:01Z'),
  });
  // The postNonIdempotent fence reads and writes heartbeats across replayed attempts.
  const call = scenarioCall(scenario, connection, 'oauth2-http', context) as Call;
  if (scenario.operation === 'postNonIdempotent') return productionMutation(client.postNonIdempotent(), call, PostNonIdempotentInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  return productionMutation(client.sendRequest(), call, SendRequestInputSchema.parse(scenario.input), scenario.idempotencyKey!);
}
