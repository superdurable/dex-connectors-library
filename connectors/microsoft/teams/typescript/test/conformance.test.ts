// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import {
  answerRecordedRequest, ExchangeRecorder, HeartbeatContext, invokeProductionMutation,
  loadScenarios, replay, scenarioCall, type Scenario,
} from '../../../../../conformance/runners/typescript/connector_scenarios.js';
import { TeamsClient } from '../src/client.js';
import {
  ListThreadRepliesInputSchema, PostChannelMessageInputSchema, PostChatMessageInputSchema,
  PostThreadReplyInputSchema,
} from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/microsoft/teams/scenarios.json', import.meta.url));
const connection = { provider: 'microsoft', name: 'teams-test' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeTeams, '/v1.0'));

function productionMutation<Input, Output>(operation: Mutation<Input, Output>, call: Call, input: Input, key: string) {
  return invokeProductionMutation(operation, key,
    recording => runMutation(recording as Mutation<Input, Output>, call, input), error => error instanceof RetryError);
}

async function invokeTeams(scenario: Scenario, recorder: ExchangeRecorder, context: HeartbeatContext) {
  const send = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const client = new TeamsClient({ endpoint: 'http://127.0.0.1:12345/v1.0' },
    { resolve: async () => ({
      oauthClientId: 'inert-client-id', oauthClientSecret: new SecretString('inert-client-secret'),
      accessToken: new SecretString('teams-access-token'), refreshToken: new SecretString('inert-refresh-token'),
    }) },
    { transport: { send }, now: () => new Date(scenario.fixedClock ?? '2026-10-01T09:30:00Z') });
  const call = scenarioCall(scenario, connection, 'microsoft-teams', context,
    { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  if (scenario.operation === 'listThreadReplies') return client.listThreadReplies().invoke(call, ListThreadRepliesInputSchema.parse(scenario.input));
  if (scenario.operation === 'postChannelMessage') return productionMutation(client.postChannelMessage(), call, PostChannelMessageInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'postThreadReply') return productionMutation(client.postThreadReply(), call, PostThreadReplyInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'postChatMessage') return productionMutation(client.postChatMessage(), call, PostChatMessageInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  throw new TypeError(`unknown Microsoft Teams operation ${scenario.operation}`);
}
