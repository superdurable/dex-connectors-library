// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { PlaidClient } from '../src/client.js';
import { GetAccountsInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/plaid/scenarios.json', import.meta.url));
const connection = { provider: 'plaid', name: 'plaid' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokePlaid, '/api'));

async function invokePlaid(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { clientId: SecretString; secret: SecretString; accessToken: SecretString }]> = [[connection, {
    clientId: new SecretString('sample-plaid-client-id'), secret: new SecretString('sample-plaid-secret'),
    accessToken: new SecretString('sample-plaid-access-token'),
  }]];
  const client = new PlaidClient({ baseUrl: 'http://127.0.0.1:12345/api' }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'plaid-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'plaid', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getAccounts') return client.getAccounts().invoke(call, GetAccountsInputSchema.parse(scenario.input));
  throw new Error(`unknown Plaid operation ${scenario.operation}`);
}
