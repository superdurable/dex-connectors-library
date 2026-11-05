// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, invokeProductionMutation, loadScenarios, replay, scenarioCall, type HeartbeatContext, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { HiverClient } from '../src/client.js';
import { AddNoteInputSchema, CreateSharedDraftInputSchema, GetConversationInputSchema, ListConversationsInputSchema, ListInboxesInputSchema, UpdateConversationInputSchema } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/hiver/scenarios.json', import.meta.url));
const connection = { provider: 'hiver', name: 'hiver-test' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeHiver, '/v1'));

function productionMutation<Input, Output>(operation: Mutation<Input, Output>, call: Call, input: Input, key: string) {
  return invokeProductionMutation(operation, key,
    recording => runMutation(recording as Mutation<Input, Output>, call, input), error => error instanceof RetryError);
}

async function invokeHiver(scenario: Scenario, recorder: ExchangeRecorder, context: HeartbeatContext) {
  const sender = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const client = new HiverClient({ requestIntervalMilliseconds: 1 }, {
    resolve: async () => ({ apiKey: new SecretString('hiverTestKey0123456789') }),
  }, { apiBaseURL: 'http://127.0.0.1:12345/v1', send: sender });
  const call = scenarioCall(scenario, connection, 'hiver', context) as Call;
  if (scenario.operation === 'listInboxes') return client.listInboxes().invoke(call, ListInboxesInputSchema.parse(scenario.input));
  if (scenario.operation === 'listConversations') return client.listConversations().invoke(call, ListConversationsInputSchema.parse(scenario.input));
  if (scenario.operation === 'getConversation') return client.getConversation().invoke(call, GetConversationInputSchema.parse(scenario.input));
  if (scenario.operation === 'updateConversation') return productionMutation(client.updateConversation(), call, UpdateConversationInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'addNote') return productionMutation(client.addNote(), call, AddNoteInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'createSharedDraft') return productionMutation(client.createSharedDraft(), call, CreateSharedDraftInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  throw new Error(`unknown Hiver operation ${scenario.operation}`);
}
