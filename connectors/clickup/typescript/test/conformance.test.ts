// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, HeartbeatContext, invokeProductionMutation, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { ClickUpClient } from '../src/client.js';
import { AddCommentInputSchema, CreateTaskInputSchema, FindMemberByEmailInputSchema, GetTaskInputSchema, SearchTasksInputSchema, UpdateTaskInputSchema, UpdateTaskTagsInputSchema } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/clickup/scenarios.json', import.meta.url));
const connection = { provider: 'clickup', name: 'clickup-test' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeClickUp, '/api/v2'));

function productionMutation<Input, Output>(operation: Mutation<Input, Output>, call: Call, input: Input, key: string) {
  return invokeProductionMutation(operation, key,
    recording => runMutation(recording as Mutation<Input, Output>, call, input), error => error instanceof RetryError);
}

async function invokeClickUp(scenario: Scenario, recorder: ExchangeRecorder, context: HeartbeatContext) {
  const sender = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const client = new ClickUpClient({}, { resolve: async () => ({ apiToken: new SecretString('pk_4411723_TESTTOKEN0123456789ABCDEFGHIJ') }) },
    { send: sender, apiBaseURL: 'http://127.0.0.1:12345/api/v2', now: () => new Date('2026-01-01T00:00:00Z') });
  const call = scenarioCall(scenario, connection, 'clickup', context,
    { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  if (scenario.operation === 'searchTasks') return client.searchTasks().invoke(call, SearchTasksInputSchema.parse(scenario.input));
  if (scenario.operation === 'getTask') return client.getTask().invoke(call, GetTaskInputSchema.parse(scenario.input));
  if (scenario.operation === 'findMemberByEmail') return client.findMemberByEmail().invoke(call, FindMemberByEmailInputSchema.parse(scenario.input));
  if (scenario.operation === 'createTask') return productionMutation(client.createTask(), call, CreateTaskInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'updateTask') return productionMutation(client.updateTask(), call, UpdateTaskInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'updateTaskTags') return productionMutation(client.updateTaskTags(), call, UpdateTaskTagsInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'addComment') return productionMutation(client.addComment(), call, AddCommentInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  throw new TypeError(`unknown ClickUp operation ${scenario.operation}`);
}
