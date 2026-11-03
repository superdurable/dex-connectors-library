// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { Client } from '../src/client.js';
import type { AddCommentInput, CreateTaskInput, GetTaskInput, ListTasksInput, UpdateTaskInput } from '../src/generated/models.js';
import { answerRecordedRequest, ExchangeRecorder, invokeProductionMutation, loadScenarios, replay, scenarioCall, type HeartbeatContext, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/asana/scenarios.json', import.meta.url));
const noProgress = { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false };

function productionMutation<Input, Output>(operation: Mutation<Input, Output>, call: Call, input: Input, key: string) {
  return invokeProductionMutation(operation, key, recording => runMutation(recording as Mutation<Input, Output>, call, input), error => error instanceof RetryError);
}

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeAsana, '/api/1.0'));

async function invokeAsana(scenario: Scenario, recorder: ExchangeRecorder, context: HeartbeatContext) {
  const sender = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const client = new Client({ endpoint: 'http://127.0.0.1:12345/api/1.0' }, {
    resolve: async () => ({ accessToken: new SecretString('asana-access-token') }),
  }, { send: sender });
  const call = scenarioCall(scenario, { provider: 'asana', name: 'asana-test' }, 'asana', context, noProgress) as Call;
  if (scenario.operation === 'listTasks') return client.listTasks().invoke(call, scenario.input as ListTasksInput);
  if (scenario.operation === 'getTask') return client.getTask().invoke(call, scenario.input as unknown as GetTaskInput);
  if (scenario.operation === 'createTask') return productionMutation(client.createTask(), call, scenario.input as unknown as CreateTaskInput, scenario.idempotencyKey!);
  if (scenario.operation === 'updateTask') return productionMutation(client.updateTask(), call, scenario.input as unknown as UpdateTaskInput, scenario.idempotencyKey!);
  if (scenario.operation === 'addComment') return productionMutation(client.addComment(), call, scenario.input as unknown as AddCommentInput, scenario.idempotencyKey!);
  throw new TypeError(`unknown operation ${scenario.operation}`);
}
