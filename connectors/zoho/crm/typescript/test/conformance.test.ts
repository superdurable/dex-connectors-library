// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, StaticCredentialProvider, type Call, type CredentialRefreshDriver, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { ZohoClient } from '../src/client.js';
import { FindRecordsInputSchema, GetRecordInputSchema, ListModifiedRecordsInputSchema, UpsertRecordInputSchema, UpdateRecordInputSchema, ListModuleFieldsInputSchema, type Credentials } from '../src/models.js';
import { answerRecordedRequest, ExchangeRecorder, HeartbeatContext, invokeProductionMutation, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/zoho/crm/scenarios.json', import.meta.url));
const connection = { provider: 'zoho', name: 'zoho-crm-main' };
const credentials: Credentials = { authMethodId: 'zoho-us-oauth', apiDomain: 'https://www.zohoapis.com',
  accessToken: new SecretString('1000.zohoCRMTestAccessToken0123456789'), oauthClientId: '1000.test-client',
  oauthClientSecret: new SecretString('test-secret'), refreshToken: new SecretString('test-refresh') };

function productionMutation<Input, Output>(operation: Mutation<Input, Output>, call: Call, input: Input, key: string) {
  return invokeProductionMutation(operation, key,
    recording => runMutation(recording as Mutation<Input, Output>, call, input), error => error instanceof RetryError);
}

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeZohoAttempt, '/crm/v8'));

async function invokeZohoAttempt(scenario: Scenario, recorder: ExchangeRecorder, context: HeartbeatContext) {
  const driver: CredentialRefreshDriver<Credentials> = { refreshRequired: () => false,
    refresh: async () => { recorder.mismatches.push('unexpected credential refresh'); throw new Error('unexpected refresh'); } };
  const transport = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const client = new ZohoClient({}, new StaticCredentialProvider([[connection, credentials]]),
    { apiBaseURL: 'http://127.0.0.1:12345/crm/v8', refreshDriver: driver, transport, now: () => new Date('2026-03-04T09:15:00Z') });
  const call = scenarioCall(scenario, connection, 'zoho-crm', context) as Call;
  if (scenario.operation === 'findRecords') return client.findRecords().invoke(call, FindRecordsInputSchema.parse(scenario.input));
  if (scenario.operation === 'getRecord') return client.getRecord().invoke(call, GetRecordInputSchema.parse(scenario.input));
  if (scenario.operation === 'listModifiedRecords') return client.listModifiedRecords().invoke(call, ListModifiedRecordsInputSchema.parse(scenario.input));
  if (scenario.operation === 'upsertRecord') return productionMutation(client.upsertRecord(), call, UpsertRecordInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'updateRecord') return productionMutation(client.updateRecord(), call, UpdateRecordInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'listModuleFields') return client.listModuleFields().invoke(call, ListModuleFieldsInputSchema.parse(scenario.input));
  throw new TypeError(`unknown operation ${scenario.operation}`);
}
