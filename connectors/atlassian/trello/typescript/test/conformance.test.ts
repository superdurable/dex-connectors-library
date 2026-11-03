// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, invokeProductionMutation, loadScenarios, replay, scenarioCall, type HeartbeatContext, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';
import { TrelloClient } from '../src/client.js';
import { AddCommentInputSchema, CreateCardInputSchema, GetCardInputSchema, ListCardsInputSchema, UpdateCardInputSchema } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/atlassian/trello/scenarios.json', import.meta.url));
const connection = { provider: 'trello', name: 'trello-test' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeTrello, '/1'));

function productionMutation<Input, Output>(operation: Mutation<Input, Output>, call: Call, input: Input, key: string) {
  return invokeProductionMutation(operation, key,
    recording => runMutation(recording as Mutation<Input, Output>, call, input), error => error instanceof RetryError);
}

async function invokeTrello(scenario: Scenario, recorder: ExchangeRecorder, context: HeartbeatContext) {
  const send = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const client = new TrelloClient({ endpoint: 'http://127.0.0.1:12345/1' }, {
    resolve: async () => ({ apiKey: new SecretString('0123456789abcdef0123456789abcdef'), token: new SecretString('ATTAtrelloTestToken0123456789abcdef') }),
  }, { send });
  const call = scenarioCall(scenario, connection, 'trello', context,
    { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  if (scenario.operation === 'listCards') return client.listCards().invoke(call, ListCardsInputSchema.parse(scenario.input));
  if (scenario.operation === 'getCard') return client.getCard().invoke(call, GetCardInputSchema.parse(scenario.input));
  if (scenario.operation === 'createCard') return productionMutation(client.createCard(), call, CreateCardInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'updateCard') return productionMutation(client.updateCard(), call, UpdateCardInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'addComment') return productionMutation(client.addComment(), call, AddCommentInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  throw new Error(`unknown Trello operation ${scenario.operation}`);
}
