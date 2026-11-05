// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, StaticCredentialProvider, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, invokeProductionMutation, loadScenarios, replay, scenarioCall, type HeartbeatContext, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { HelpScoutClient } from '../src/client.js';
import { FindCustomerByEmailInputSchema, GetConversationInputSchema, ReplyToConversationInputSchema, SearchConversationsInputSchema, UpdateConversationInputSchema } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/helpscout/scenarios.json', import.meta.url));
const connection = { provider: 'helpscout', name: 'support-inbox' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeHelpScout));

function productionMutation<Input, Output>(operation: Mutation<Input, Output>, call: Call, input: Input, key: string) {
  return invokeProductionMutation(operation, key,
    recording => runMutation(recording as Mutation<Input, Output>, call, input), error => error instanceof RetryError);
}

async function invokeHelpScout(scenario: Scenario, recorder: ExchangeRecorder, context: HeartbeatContext) {
  const sender = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const client = new HelpScoutClient({}, new StaticCredentialProvider([[connection, {
    appId: 'app-id', appSecret: new SecretString('app-secret'), accessToken: new SecretString('SENTINEL-HELPSCOUT-ACCESS-TOKEN'), webhookSecret: new SecretString('inert-webhook-secret'),
  }]]), { send: sender, now: () => new Date('2026-09-30T12:00:00Z') });
  const call = scenarioCall(scenario, connection, 'helpscout', context) as Call;
  if (scenario.operation === 'searchConversations') return client.searchConversations().invoke(call, SearchConversationsInputSchema.parse(scenario.input));
  if (scenario.operation === 'getConversation') return client.getConversation().invoke(call, GetConversationInputSchema.parse(scenario.input));
  if (scenario.operation === 'replyToConversation') return productionMutation(client.replyToConversation(), call, ReplyToConversationInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'updateConversation') return productionMutation(client.updateConversation(), call, UpdateConversationInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'findCustomerByEmail') return client.findCustomerByEmail().invoke(call, FindCustomerByEmailInputSchema.parse(scenario.input));
  throw new Error(`unknown Help Scout operation ${scenario.operation}`);
}
