// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, StaticCredentialProvider, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, invokeProductionMutation, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';
import { OAuth2HTTPClient } from '../src/client.js';
import { SendRequestInputSchema, type SendRequestInput, type SendRequestOutput } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/superdurable/oauth2-http/scenarios.json', import.meta.url));
const connection = { provider: 'oauth2-http', name: 'protected-api' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeOAuth2HTTP, '/v1'));

async function invokeOAuth2HTTP(scenario: Scenario, recorder: ExchangeRecorder) {
  const send = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const client = new OAuth2HTTPClient({
    authorizationEndpoint: 'http://127.0.0.1:12345/auth', tokenEndpoint: 'http://127.0.0.1:12345/token',
    scopes: ['profile'], apiBaseUrl: 'http://127.0.0.1:12345/v1',
  }, new StaticCredentialProvider([[connection, { oauthClientId: 'dex-test-client', oauthClientSecret: new SecretString('SENTINEL-CLIENT-SECRET'),
    accessToken: new SecretString('conformance-access-token'), refreshToken: new SecretString('') }]]), {
    send, now: () => new Date(scenario.fixedClock ?? '1970-01-01T00:00:01Z'),
  });
  const call = scenarioCall(scenario, connection, 'oauth2-http', {
    flowId: 'oauth2-http-conformance', cancellationSignal: new AbortController().signal,
  }, { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  const operation = client.sendRequest();
  const input = SendRequestInputSchema.parse(scenario.input);
  return invokeProductionMutation(operation, scenario.idempotencyKey!, recording =>
    runMutation(recording as Mutation<SendRequestInput, SendRequestOutput>, call, input), error => error instanceof RetryError);
}
