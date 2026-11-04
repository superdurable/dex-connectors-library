// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, invokeProductionMutation, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { AirtableClient } from '../src/client.js';
import { GetRecordInputSchema, ListRecordsInputSchema, UpdateRecordsInputSchema, UpsertRecordsInputSchema, type UpdateRecordsInput, type UpsertRecordsInput } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/airtable/scenarios.json', import.meta.url));
const connection = { provider: 'airtable', name: 'airtable-refund-policies' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeAirtable));

function productionMutation<Input, Output>(operation: Mutation<Input, Output>, call: Call, input: Input, key: string) {
  return invokeProductionMutation(operation, key,
    recording => runMutation(recording as Mutation<Input, Output>, call, input), error => error instanceof RetryError);
}

async function invokeAirtable(scenario: Scenario, recorder: ExchangeRecorder) {
  const sender = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const client = new AirtableClient({ endpoint: 'http://127.0.0.1:12345' }, {
    resolve: async () => ({ personalAccessToken: new SecretString('patTESTtoken.0123456789abcdef') }),
  }, { send: sender });
  const call = scenarioCall(scenario, connection, 'airtable', {
    flowId: 'refund-flow', cancellationSignal: new AbortController().signal,
  }, { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  if (scenario.operation === 'listRecords') return client.listRecords().invoke(call, ListRecordsInputSchema.parse(scenario.input));
  if (scenario.operation === 'getRecord') return client.getRecord().invoke(call, GetRecordInputSchema.parse(scenario.input));
  if (scenario.operation === 'upsertRecords') return productionMutation(client.upsertRecords(), call, UpsertRecordsInputSchema.parse(scenario.input) as UpsertRecordsInput, scenario.idempotencyKey!);
  if (scenario.operation === 'updateRecords') return productionMutation(client.updateRecords(), call, UpdateRecordsInputSchema.parse(scenario.input) as UpdateRecordsInput, scenario.idempotencyKey!);
  throw new Error(`unknown Airtable operation ${scenario.operation}`);
}
