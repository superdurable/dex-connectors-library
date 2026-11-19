// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, HeartbeatContext, invokeProductionMutation, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { FrontClient } from '../src/client.js';
import { FindContactByEmailInputSchema, GetConversationInputSchema, ReplyToConversationInputSchema, SearchConversationsInputSchema, UpdateConversationInputSchema } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/front/scenarios.json', import.meta.url));
const connection = { provider: 'front', name: 'front-test' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay({
  ...scenario,
  exchanges: scenario.exchanges.map(exchange => ({ ...exchange })),
}, invokeFront));

function productionMutation<Input, Output>(operation: Mutation<Input, Output>, call: Call, input: Input, key: string) {
  return invokeProductionMutation(operation, key,
    recording => runMutation(recording as Mutation<Input, Output>, call, input), error => error instanceof RetryError);
}

async function invokeFront(scenario: Scenario, recorder: ExchangeRecorder, context: HeartbeatContext) {
  const sender = (request: ProviderRequest) => {
    const target = new URL(request.url);
    const exchange = scenario.exchanges[recorder.requestCount];
    if (exchange !== undefined && decodeURIComponent(target.pathname) !== exchange.path) {
      throw new Error(`decoded Front path ${decodeURIComponent(target.pathname)} differs from ${exchange.path}`);
    }
    // The shared recorder parses URL.pathname, while Go records a decoded path.
    if (exchange !== undefined) exchange.path = target.pathname;
    return answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  };
  const client = new FrontClient({}, { resolve: async () => ({ apiToken: new SecretString('front-unit-test-token-0123456789') }) },
    { apiBaseURL: 'http://127.0.0.1:12345', send: sender });
  const call = scenarioCall(scenario, connection, 'front', context,
    { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  if (scenario.operation === 'searchConversations') return client.searchConversations().invoke(call, SearchConversationsInputSchema.parse(scenario.input));
  if (scenario.operation === 'getConversation') return client.getConversation().invoke(call, GetConversationInputSchema.parse(scenario.input));
  if (scenario.operation === 'replyToConversation') return productionMutation(client.replyToConversation(), call, ReplyToConversationInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'updateConversation') return productionMutation(client.updateConversation(), call, UpdateConversationInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'findContactByEmail') return client.findContactByEmail().invoke(call, FindContactByEmailInputSchema.parse(scenario.input));
  throw new Error(`unknown Front operation ${scenario.operation}`);
}
