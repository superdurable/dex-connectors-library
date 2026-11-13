// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, invokeProductionMutation, loadScenarios, replay, scenarioCall, type HeartbeatContext, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { SlackClient } from '../src/client.js';
import { GetThreadReplyInputSchema, GetWorkspaceIdentityInputSchema, ListChannelMessagesInputSchema, ListChannelsInputSchema, ListThreadMessagesInputSchema, PostChannelMessageInputSchema, PostThreadReplyInputSchema } from '../src/generated/models.js';

// postButtonMessage is Go only; replay just the operations this package implements.
const typescriptOperations = new Set([
  'listChannels', 'getWorkspaceIdentity', 'listThreadMessages', 'getThreadReply', 'listChannelMessages',
  'postChannelMessage', 'postThreadReply',
]);
const scenarios = loadScenarios(new URL('../../../../conformance/connectors/slack/scenarios.json', import.meta.url))
  .filter(scenario => typescriptOperations.has(scenario.operation));
const connection = { provider: 'slack', name: 'workspace' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeSlack, '/api'));

function productionMutation<Input, Output>(operation: Mutation<Input, Output>, call: Call, input: Input, key: string) {
  return invokeProductionMutation(operation, key,
    recording => runMutation(recording as Mutation<Input, Output>, call, input), error => error instanceof RetryError);
}

async function invokeSlack(scenario: Scenario, recorder: ExchangeRecorder, context: HeartbeatContext) {
  const sender = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const botToken = scenario.name === 'getWorkspaceIdentity-defect' ? '' : 'bot-token';
  const client = new SlackClient({}, {
    resolve: async () => ({ botToken: new SecretString(botToken), userToken: new SecretString('user-token') }),
    resolveWithRefresh: async () => ({ botToken: new SecretString(botToken), userToken: new SecretString('user-token') }),
  }, { send: sender });
  const call = scenarioCall(scenario, connection, 'slack', context) as Call;
  if (scenario.operation === 'listChannels') return client.listChannels().invoke(call, ListChannelsInputSchema.parse(scenario.input));
  if (scenario.operation === 'getWorkspaceIdentity') return client.getWorkspaceIdentity().invoke(call, GetWorkspaceIdentityInputSchema.parse(scenario.input));
  if (scenario.operation === 'listThreadMessages') return client.listThreadMessages().invoke(call, ListThreadMessagesInputSchema.parse(scenario.input));
  if (scenario.operation === 'getThreadReply') return client.getThreadReply().invoke(call, GetThreadReplyInputSchema.parse(scenario.input));
  if (scenario.operation === 'listChannelMessages') return client.listChannelMessages().invoke(call, ListChannelMessagesInputSchema.parse(scenario.input));
  if (scenario.operation === 'postChannelMessage') return productionMutation(client.postChannelMessage(), call, PostChannelMessageInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'postThreadReply') return productionMutation(client.postThreadReply(), call, PostThreadReplyInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  throw new Error(`unknown Slack operation ${scenario.operation}`);
}
