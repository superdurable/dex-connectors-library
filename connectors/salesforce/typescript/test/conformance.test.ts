// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import {
  RetryError, runMutation, SecretString, StaticCredentialProvider,
  type Call, type Mutation,
} from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import {
  answerRecordedRequest, ExchangeRecorder, invokeProductionMutation, loadScenarios,
  replay, scenarioCall, type Scenario,
} from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { SalesforceClient } from '../src/client.js';
import {
  GetRecordInputSchema, QueryRecordsInputSchema, UpdateRecordInputSchema,
  UpsertRecordByExternalIDInputSchema,
} from '../src/models.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/salesforce/scenarios.json', import.meta.url));
const connection = { provider: 'salesforce', name: 'salesforce-crm' };

function productionMutation<Input, Output>(operation: Mutation<Input, Output>, call: Call, input: Input, key: string) {
  return invokeProductionMutation(operation, key,
    recording => runMutation(recording as Mutation<Input, Output>, call, input),
    error => error instanceof RetryError);
}

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeSalesforce));

async function invokeSalesforce(scenario: Scenario, recorder: ExchangeRecorder) {
  const send = (request: ProviderRequest) => answerRecordedRequest(
    recorder, request, before => new ProviderTransportError(before),
  );
  const credentials = {
    authMethodId: 'salesforce-oauth', oauthClientId: 'inert-client-id',
    accessToken: new SecretString('salesforce-token-SENTINEL'),
    instanceUrl: 'http://127.0.0.1:12345',
  };
  const client = new SalesforceClient({}, new StaticCredentialProvider([[connection, credentials]]), { send });
  const call = scenarioCall(scenario, connection, 'salesforce', {
    cancellationSignal: new AbortController().signal,
  }) as Call;
  if (scenario.operation === 'queryRecords') return client.queryRecords().invoke(call, QueryRecordsInputSchema.parse(scenario.input));
  if (scenario.operation === 'getRecord') return client.getRecord().invoke(call, GetRecordInputSchema.parse(scenario.input));
  if (scenario.operation === 'upsertRecordByExternalId') return productionMutation(
    client.upsertRecordByExternalId(), call, UpsertRecordByExternalIDInputSchema.parse(scenario.input), scenario.idempotencyKey!,
  );
  if (scenario.operation === 'updateRecord') return productionMutation(
    client.updateRecord(), call, UpdateRecordInputSchema.parse(scenario.input), scenario.idempotencyKey!,
  );
  throw new Error(`unknown Salesforce operation ${scenario.operation}`);
}
