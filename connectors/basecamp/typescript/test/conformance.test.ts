// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { BasecampClient } from '../src/client.js';
import { ListAccountsInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/basecamp/scenarios.json', import.meta.url));
const connection = { provider: 'basecamp', name: 'basecamp' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeBasecamp, ''));

async function invokeBasecamp(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials = {
    oauthClientId: 'synthetic-client-id', oauthClientSecret: new SecretString('synthetic-client-secret'),
    accessToken: new SecretString('test-basecamp-token'), refreshToken: new SecretString('synthetic-refresh-token'),
  };
  const client = new BasecampClient({ baseUrl: 'http://127.0.0.1:12345' }, new StaticCredentialProvider([[connection, credentials]]), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'basecamp-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'basecamp', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'listAccounts') return client.listAccounts().invoke(call, ListAccountsInputSchema.parse(scenario.input));
  throw new Error(`unknown Basecamp operation ${scenario.operation}`);
}
