// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { RedditClient } from '../src/client.js';
import { GetPostInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/reddit/scenarios.json', import.meta.url));
const connection = { provider: 'reddit', name: 'reddit' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeReddit, ''));

async function invokeReddit(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials = {
    oauthClientId: 'synthetic-client-id', oauthClientSecret: new SecretString('synthetic-client-secret'),
    accessToken: new SecretString('test-reddit-token'), refreshToken: new SecretString('synthetic-refresh-token'),
  };
  const client = new RedditClient({ baseUrl: 'http://127.0.0.1:12345', redditUsername: 'example' }, new StaticCredentialProvider([[connection, credentials]]), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'reddit-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'reddit', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getPost') return client.getPost().invoke(call, GetPostInputSchema.parse(scenario.input));
  throw new Error(`unknown Reddit operation ${scenario.operation}`);
}
