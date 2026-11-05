// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, invokeProductionMutation, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';
import { WorkspaceAdminClient } from '../src/client.js';
import { AddUserToGroupInputSchema, CreateUserInputSchema, GetUserInputSchema, ListUsersInputSchema, RemoveUserFromGroupInputSchema, SuspendUserInputSchema, UnsuspendUserInputSchema } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/google/workspace-admin/scenarios.json', import.meta.url));
const connection = { provider: 'google', name: 'google-workspace-admin-test' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeWorkspaceAdmin, '/admin/directory/v1'));

function productionMutation<Input, Output>(operation: Mutation<Input, Output>, call: Call, input: Input, key: string) {
  return invokeProductionMutation(operation, key, recording => runMutation(recording as Mutation<Input, Output>, call, input), error => error instanceof RetryError);
}

async function invokeWorkspaceAdmin(scenario: Scenario, recorder: ExchangeRecorder) {
  const sender = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const client = new WorkspaceAdminClient({ endpoint: 'http://127.0.0.1:12345', ...(scenario.name.endsWith('oversized') ? { maxResponseBytes: 1 } : {}) }, {
    resolve: async () => ({ accessToken: new SecretString('directory-token') }),
    resolveWithRefresh: async () => ({ accessToken: new SecretString('directory-token') }),
  }, { send: sender, randomBytes: length => new Uint8Array(length).fill(0x42) });
  const call = scenarioCall(scenario, connection, 'google-workspace-admin', {
    flowId: 'workspace-admin-flow', firstAttemptAt: new Date().toISOString(), cancellationSignal: new AbortController().signal,
  }, { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  if (scenario.operation === 'getUser') return client.getUser().invoke(call, GetUserInputSchema.parse(scenario.input));
  if (scenario.operation === 'listUsers') return client.listUsers().invoke(call, ListUsersInputSchema.parse(scenario.input));
  if (scenario.operation === 'createUser') return productionMutation(client.createUser(), call, CreateUserInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'suspendUser') return productionMutation(client.suspendUser(), call, SuspendUserInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'unsuspendUser') return productionMutation(client.unsuspendUser(), call, UnsuspendUserInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'addUserToGroup') return productionMutation(client.addUserToGroup(), call, AddUserToGroupInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'removeUserFromGroup') return productionMutation(client.removeUserFromGroup(), call, RemoveUserFromGroupInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  throw new Error(`unknown Google Workspace Admin operation ${scenario.operation}`);
}
