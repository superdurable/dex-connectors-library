// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, invokeProductionMutation, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';
import { TodoistClient } from '../src/client.js';
import { CreateTaskInputSchema, DeleteTaskInputSchema, GetTaskInputSchema, ListTasksInputSchema, UpdateTaskInputSchema } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/doist/todoist/scenarios.json', import.meta.url));
const connection = { provider: 'todoist', name: 'todoist' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeTodoist));

function productionMutation<Input, Output>(operation: Mutation<Input, Output>, call: Call, input: Input, key: string) {
  return invokeProductionMutation(operation, key,
    recording => runMutation(recording as Mutation<Input, Output>, call, input), error => error instanceof RetryError);
}

async function invokeTodoist(scenario: Scenario, recorder: ExchangeRecorder) {
  const sender = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const client = new TodoistClient({ baseUrl: 'http://127.0.0.1:12345/api/v1' }, {
    resolve: async () => ({ apiToken: new SecretString('todoist-test-token-never-persist') }),
  }, { send: sender });
  const call = scenarioCall(scenario, connection, 'todoist', {
    flowId: 'provider-test', deadline: new Date(Date.now() + 30_000), cancellationSignal: new AbortController().signal,
  }, { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  if (scenario.operation === 'getTask') return client.getTask().invoke(call, GetTaskInputSchema.parse(scenario.input));
  if (scenario.operation === 'updateTaskContent') return productionMutation(client.updateTaskContent(), call, UpdateTaskInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'listTasks') return client.listTasks().invoke(call, ListTasksInputSchema.parse(scenario.input));
  if (scenario.operation === 'createTask') return productionMutation(client.createTask(), call, CreateTaskInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'deleteTask') return productionMutation(client.deleteTask(), call, DeleteTaskInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  throw new Error(`unknown Todoist operation ${scenario.operation}`);
}
