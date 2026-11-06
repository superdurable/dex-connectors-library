// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, invokeProductionMutation, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';
import { ZohoClient } from '../src/client.js';
import { FindRecordsInputSchema, GetRecordInputSchema, ListModifiedRecordsInputSchema, ListModuleFieldsInputSchema, UpdateRecordInputSchema, UpsertRecordInputSchema } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/zoho/crm/scenarios.json', import.meta.url));
const connection = { provider: 'zoho', name: 'zoho-crm' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeZoho, '/crm/v8'));

function productionMutation<Input, Output>(operation: Mutation<Input, Output>, call: Call, input: Input, key: string) {
  return invokeProductionMutation(operation, key,
    recording => runMutation(recording as Mutation<Input, Output>, call, input), error => error instanceof RetryError);
}

async function invokeZoho(scenario: Scenario, recorder: ExchangeRecorder) {
  const transport = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const credentials = {
    authMethodId: 'zoho-us-oauth', apiDomain: 'https://www.zohoapis.com',
    oauthClientId: 'synthetic-client', oauthClientSecret: new SecretString('synthetic-secret'),
    accessToken: new SecretString('synthetic-access'), refreshToken: new SecretString('synthetic-refresh'),
  };
  const client = new ZohoClient({}, { resolve: async () => credentials, resolveWithRefresh: async () => credentials },
    { apiBaseURL: 'http://127.0.0.1:47101/crm/v8', transport, now: () => new Date('2026-10-30T00:00:00Z') });
  const call = scenarioCall(scenario, connection, 'zoho-crm', {
    flowId: 'zoho-crm-conformance', cancellationSignal: new AbortController().signal,
  }, { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  if (scenario.operation === 'findRecords') return client.findRecords().invoke(call, FindRecordsInputSchema.parse(scenario.input));
  if (scenario.operation === 'getRecord') return client.getRecord().invoke(call, GetRecordInputSchema.parse(scenario.input));
  if (scenario.operation === 'listModifiedRecords') return client.listModifiedRecords().invoke(call, ListModifiedRecordsInputSchema.parse(scenario.input));
  if (scenario.operation === 'listModuleFields') return client.listModuleFields().invoke(call, ListModuleFieldsInputSchema.parse(scenario.input));
  if (scenario.operation === 'upsertRecord') return productionMutation(client.upsertRecord(), call, UpsertRecordInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'updateRecord') return productionMutation(client.updateRecord(), call, UpdateRecordInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  throw new Error(`unknown operation ${scenario.operation}`);
}
