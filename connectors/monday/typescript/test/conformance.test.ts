// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, invokeProductionMutation, loadScenarios, replay, scenarioCall, type HeartbeatContext, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { MondayClient } from '../src/client.js';
import { AddUpdateInputSchema, CreateItemInputSchema, GetItemInputSchema, ListItemsInputSchema, UpdateItemColumnValuesInputSchema, type AddUpdateInput, type CreateItemInput, type UpdateItemColumnValuesInput } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/monday/scenarios.json', import.meta.url));
const connection = { provider: 'monday', name: 'monday-test' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeMonday));

function productionMutation<Input, Output>(operation: Mutation<Input, Output>, call: Call, input: Input, key: string) {
  return invokeProductionMutation(operation, key,
    recording => runMutation(recording as Mutation<Input, Output>, call, input), error => error instanceof RetryError);
}

async function invokeMonday(scenario: Scenario, recorder: ExchangeRecorder, context: HeartbeatContext) {
  const sender = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const client = new MondayClient({}, {
    resolve: async () => ({ authMethodId: 'personal-api-token', apiToken: new SecretString('mondayTestToken0123456789abcdefSENTINELKEY') }),
    resolveWithRefresh: async () => ({ authMethodId: 'personal-api-token', apiToken: new SecretString('mondayTestToken0123456789abcdefSENTINELKEY') }),
  }, { apiURL: 'http://127.0.0.1/v2', send: sender, now: () => new Date(scenario.fixedClock ?? '2026-10-01T00:00:00Z') });
  const call = scenarioCall(scenario, connection, 'monday', context,
    { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  if (scenario.operation === 'listItems') return client.listItems().invoke(call, ListItemsInputSchema.parse(scenario.input));
  if (scenario.operation === 'getItem') return client.getItem().invoke(call, GetItemInputSchema.parse(scenario.input));
  if (scenario.operation === 'createItem') return productionMutation(client.createItem(), call, CreateItemInputSchema.parse(scenario.input) as CreateItemInput, scenario.idempotencyKey!);
  if (scenario.operation === 'updateItemColumnValues') return productionMutation(client.updateItemColumnValues(), call, UpdateItemColumnValuesInputSchema.parse(scenario.input) as UpdateItemColumnValuesInput, scenario.idempotencyKey!);
  if (scenario.operation === 'addUpdate') return productionMutation(client.addUpdate(), call, AddUpdateInputSchema.parse(scenario.input) as AddUpdateInput, scenario.idempotencyKey!);
  throw new Error(`unknown monday.com operation ${scenario.operation}`);
}
