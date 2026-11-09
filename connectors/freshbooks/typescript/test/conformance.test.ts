// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { FreshBooksClient } from '../src/client.js';
import { GetInvoiceInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/freshbooks/scenarios.json', import.meta.url));
const connection = { provider: 'freshbooks', name: 'freshbooks' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeFreshBooks, ''));

async function invokeFreshBooks(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials = {
    oauthClientId: 'synthetic-client-id', oauthClientSecret: new SecretString('synthetic-client-secret'),
    accessToken: new SecretString('test-freshbooks-token'), refreshToken: new SecretString('synthetic-refresh-token'),
  };
  const client = new FreshBooksClient({ redirectUri: 'https://dex.example/callback', baseUrl: 'http://127.0.0.1:12345' }, new StaticCredentialProvider([[connection, credentials]]), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'freshbooks-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'freshbooks', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getInvoice') return client.getInvoice().invoke(call, GetInvoiceInputSchema.parse(scenario.input));
  throw new Error(`unknown FreshBooks operation ${scenario.operation}`);
}
