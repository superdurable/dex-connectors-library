// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { BigQueryClient } from '../src/client.js';
import { ListDatasetsInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/google/bigquery/scenarios.json', import.meta.url));
const connection = { provider: 'google-bigquery', name: 'google-bigquery' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeBigQuery, ''));

async function invokeBigQuery(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials = {
    oauthClientId: 'synthetic-client-id', oauthClientSecret: new SecretString('synthetic-client-secret'),
    accessToken: new SecretString('test-google-bigquery-token'), refreshToken: new SecretString('synthetic-refresh-token'),
  };
  const client = new BigQueryClient({ baseUrl: 'http://127.0.0.1:12345' }, new StaticCredentialProvider([[connection, credentials]]), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'google-bigquery-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'google-bigquery', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'listDatasets') return client.listDatasets().invoke(call, ListDatasetsInputSchema.parse(scenario.input));
  throw new Error(`unknown BigQuery operation ${scenario.operation}`);
}
