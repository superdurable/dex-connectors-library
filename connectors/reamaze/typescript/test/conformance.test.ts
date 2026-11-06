// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, HeartbeatContext, invokeProductionMutation, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { ReamazeClient } from '../src/client.js';
import { CreateConversationInputSchema, FindContactByEmailInputSchema, GetConversationInputSchema, ReplyToConversationInputSchema, SearchConversationsInputSchema, UpdateConversationInputSchema } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/reamaze/scenarios.json', import.meta.url));
const connection = { provider: 'reamaze', name: 'reamaze-test' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeReamaze, '/api/v1'));

function productionMutation<Input, Output>(operation: Mutation<Input, Output>, call: Call, input: Input, key: string) {
  return invokeProductionMutation(operation, key,
    recording => runMutation(recording as Mutation<Input, Output>, call, input), error => error instanceof RetryError);
}

async function invokeReamaze(scenario: Scenario, recorder: ExchangeRecorder, context: HeartbeatContext) {
  const sender = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const client = new ReamazeClient({ brand: 'acme' }, { resolve: async () => ({ email: 'agent@acme.example.com', apiToken: new SecretString('reamazeTestToken0123456789') }) }, { apiBaseURL: 'http://localhost/api/v1', send: sender });
  const call = scenarioCall(scenario, connection, 'reamaze', context) as Call;
  if (scenario.operation === 'searchConversations') return client.searchConversations().invoke(call, SearchConversationsInputSchema.parse(scenario.input));
  if (scenario.operation === 'getConversation') return client.getConversation().invoke(call, GetConversationInputSchema.parse(scenario.input));
  if (scenario.operation === 'findContactByEmail') return client.findContactByEmail().invoke(call, FindContactByEmailInputSchema.parse(scenario.input));
  if (scenario.operation === 'createConversation') return productionMutation(client.createConversation(), call, CreateConversationInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'updateConversation') return productionMutation(client.updateConversation(), call, UpdateConversationInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'replyToConversation') return productionMutation(client.replyToConversation(), call, ReplyToConversationInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  throw new Error(`unknown Re:amaze operation ${scenario.operation}`);
}
