// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, invokeProductionMutation, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { DiscordClient } from '../src/client.js';
import { AddReactionInputSchema, CreateMessageInputSchema, GetChannelInputSchema, ListChannelMessagesInputSchema } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/discord/scenarios.json', import.meta.url));
const connection = { provider: 'discord', name: 'discord' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeDiscord));

function productionMutation<Input, Output>(operation: Mutation<Input, Output>, call: Call, input: Input, key: string) {
  return invokeProductionMutation(operation, key,
    recording => runMutation(recording as Mutation<Input, Output>, call, input), error => error instanceof RetryError);
}

async function invokeDiscord(scenario: Scenario, recorder: ExchangeRecorder) {
  const sender = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const client = new DiscordClient({ endpoint: 'http://127.0.0.1:12345' }, {
    resolve: async () => ({ botToken: new SecretString('MTAxTESTbot.token-part.0123456789abcdef') }),
  }, { send: sender });
  const call = scenarioCall(scenario, connection, 'discord', {
    flowId: 'channel-announcement-flow', cancellationSignal: new AbortController().signal,
  }, { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  if (scenario.operation === 'getChannel') return client.getChannel().invoke(call, GetChannelInputSchema.parse(scenario.input));
  if (scenario.operation === 'listChannelMessages') return client.listChannelMessages().invoke(call, ListChannelMessagesInputSchema.parse(scenario.input));
  if (scenario.operation === 'createMessage') return productionMutation(client.createMessage(), call, CreateMessageInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'addReaction') return productionMutation(client.addReaction(), call, AddReactionInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  throw new Error(`unknown Discord operation ${scenario.operation}`);
}
