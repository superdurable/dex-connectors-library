// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, HeartbeatContext, SyntheticRandomSource, invokeProductionMutation, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';
import { EntraClient } from '../src/client.js';
import { AddUserToGroupInputSchema, CreateUserInputSchema, DisableUserInputSchema, EnableUserInputSchema, GetUserInputSchema, ListUsersInputSchema, RemoveUserFromGroupInputSchema, RevokeSignInSessionsInputSchema } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/microsoft/entra-id/scenarios.json', import.meta.url));
const connection = { provider: 'microsoft', name: 'entra-conformance' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeEntra, '/v1.0'));

function productionMutation<Input, Output>(operation: Mutation<Input, Output>, call: Call, input: Input, key: string) {
  return invokeProductionMutation(operation, key, recording => runMutation(recording as Mutation<Input, Output>, call, input), error => error instanceof RetryError);
}

async function invokeEntra(scenario: Scenario, recorder: ExchangeRecorder, context: HeartbeatContext) {
  const sender = (request: ProviderRequest) => {
    if (scenario.name === 'getUser-special-upn' && !request.url.includes('%27%27')) throw new TypeError('Graph path did not use Go PathEscape');
    return answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  };
  const randomSource = scenario.randomSeed ? new SyntheticRandomSource(scenario.randomSeed) : undefined;
  const credentials = { authMethodId: 'microsoft-oauth', accessToken: new SecretString('entra-token'), clientId: 'inert-client', clientSecret: new SecretString('inert-secret'), refreshToken: new SecretString('inert-refresh') };
  const provider = scenario.name === 'getUser-401-renewed'
    ? { resolve: async () => ({ ...credentials, accessToken: new SecretString('stale-token') }), resolveWithRefresh: async () => ({ ...credentials, accessToken: new SecretString('stale-token') }), resolveAfterRejection: async () => ({ ...credentials, accessToken: new SecretString('fresh-token') }) }
    : { resolve: async () => credentials, resolveWithRefresh: async () => credentials };
  const client = new EntraClient({ maxResponseBytes: scenario.name === 'removeUserFromGroup-invalid-response' ? 2 : 4_194_304 }, provider,
    { send: sender, graphBaseURL: 'http://127.0.0.1:12345/v1.0', now: () => new Date('2026-01-01T00:00:00Z'), ...(randomSource ? { randomInt: maximum => randomSource.goRandomInt(maximum) } : {}) });
  if (scenario.name.includes('propagation')) Object.assign(context, { firstAttemptAt: new Date('2026-01-01T00:00:00Z') });
  const call = scenarioCall(scenario, connection, 'microsoft-entra-id', context,
    { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  if (scenario.operation === 'getUser') return client.getUser().invoke(call, GetUserInputSchema.parse(scenario.input));
  if (scenario.operation === 'listUsers') return client.listUsers().invoke(call, ListUsersInputSchema.parse(scenario.input));
  if (scenario.operation === 'createUser') return productionMutation(client.createUser(), call, CreateUserInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'disableUser') return productionMutation(client.disableUser(), call, DisableUserInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'enableUser') return productionMutation(client.enableUser(), call, EnableUserInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'revokeSignInSessions') return productionMutation(client.revokeSignInSessions(), call, RevokeSignInSessionsInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'addUserToGroup') return productionMutation(client.addUserToGroup(), call, AddUserToGroupInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'removeUserFromGroup') return productionMutation(client.removeUserFromGroup(), call, RemoveUserFromGroupInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  throw new TypeError(`unknown Entra operation ${scenario.operation}`);
}
