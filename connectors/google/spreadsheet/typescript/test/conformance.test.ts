// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { RetryError, runMutation, SecretString, type Call, type Mutation, type MutationAttempt } from '@superdurable/connectors-sdk';
import { answerRecordedRequest, ExchangeRecorder, invokeProductionMutation, loadScenarios, replay, scenarioCall, type HeartbeatContext, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';
import { SpreadsheetClient } from '../src/client.js';
import { GetValuesInputSchema, FindRowInputSchema, UpsertRowInputSchema, type Credentials, type UpsertRowInput, type UpsertRowOutput } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/google/spreadsheet/scenarios.json', import.meta.url));
const connection = { provider: 'google', name: 'customer-sheet' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeSpreadsheet));

async function invokeSpreadsheet(scenario: Scenario, recorder: ExchangeRecorder, context: HeartbeatContext) {
  const sender = (request: ProviderRequest) => {
    const pathname = new URL(request.url).pathname;
    const exchange = scenario.exchanges[recorder.requestCount];
    if (exchange && decodeURIComponent(pathname) !== exchange.path) throw new Error('Google Sheets request path differs from Go');
    return answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  };
  const credential: Credentials = {
    accessToken: new SecretString('sheets-token'), oauthClientId: 'test-client',
    oauthClientSecret: new SecretString('test-secret'), refreshToken: new SecretString('test-refresh'),
  };
  const client = new SpreadsheetClient({ endpoint: 'http://127.0.0.1:12345' }, {
    resolve: async () => credential,
    resolveWithRefresh: async () => credential,
  }, { send: sender });
  const call = scenarioCall(scenario, connection, 'google-sheets', context, {
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  }) as Call;
  if (scenario.operation === 'getValues') return client.getValues().invoke(call, GetValuesInputSchema.parse(scenario.input));
  if (scenario.operation === 'findRow') return client.findRow().invoke(call, FindRowInputSchema.parse(scenario.input));
  if (scenario.operation === 'upsertRow') {
    const operation = client.upsertRow();
    const input = UpsertRowInputSchema.parse(scenario.input) as UpsertRowInput;
    return invokeProductionMutation<UpsertRowInput, MutationAttempt<UpsertRowOutput>, Call>(operation, scenario.idempotencyKey!,
      recording => runMutation(recording as Mutation<UpsertRowInput, UpsertRowOutput>, call, input),
      error => error instanceof RetryError);
  }
  throw new Error(`unknown Google Sheets operation ${scenario.operation}`);
}
