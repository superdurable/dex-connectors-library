// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { MicrosoftTodoClient } from '../src/client.js';
import { ListTaskListsInputSchema, ListTasksInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/microsoft/todo/scenarios.json', import.meta.url));
const connection = { provider: 'microsoft', name: 'microsoft-todo' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeTodo, '/v1.0'));

async function invokeTodo(scenario: Scenario, recorder: ExchangeRecorder) {
  const token = scenario.name === 'listTaskLists-defect' ? '' : 'sample-access-token';
  const credentials: Array<[typeof connection, { oauthClientId: string; oauthClientSecret: SecretString; accessToken: SecretString; refreshToken: SecretString }]> = [[connection, {
    oauthClientId: 'sample-client-id', oauthClientSecret: new SecretString('sample-client-secret'),
    accessToken: new SecretString(token), refreshToken: new SecretString('sample-refresh-token'),
  }]];
  const client = new MicrosoftTodoClient({}, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'microsoft-todo-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'microsoft-todo', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'listTaskLists') return client.listTaskLists().invoke(call, ListTaskListsInputSchema.parse(scenario.input));
  if (scenario.operation === 'listTasks') return client.listTasks().invoke(call, ListTasksInputSchema.parse(scenario.input));
  throw new Error(`unknown Microsoft To Do operation ${scenario.operation}`);
}
