// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, HeartbeatContext, invokeProductionMutation, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { NotionClient } from '../src/client.js';
import { CreatePageInputSchema, GetPageInputSchema, QueryDatabaseInputSchema, SearchInputSchema, UpdatePagePropertiesInputSchema, type CreatePageInput, type UpdatePagePropertiesInput } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/notion/scenarios.json', import.meta.url));
const connection = { provider: 'notion', name: 'notion-test' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeNotion, '/v1'));

function productionMutation<Input, Output>(operation: Mutation<Input, Output>, call: Call, input: Input, key: string) {
  return invokeProductionMutation(operation, key, recording => runMutation(recording as Mutation<Input, Output>, call, input), error => error instanceof RetryError);
}

async function invokeNotion(scenario: Scenario, recorder: ExchangeRecorder) {
  const sender = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const client = new NotionClient({ endpoint: 'http://127.0.0.1' }, {
    resolve: async () => ({ apiToken: new SecretString('ntn_test_token_value') }),
  }, { send: sender });
  const context = new HeartbeatContext(scenario.priorHeartbeat);
  const call = scenarioCall(scenario, connection, 'notion', context, {
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  }) as Call;
  if (scenario.operation === 'search') return client.search().invoke(call, SearchInputSchema.parse(scenario.input));
  if (scenario.operation === 'queryDatabase') return client.queryDatabase().invoke(call, QueryDatabaseInputSchema.parse(scenario.input));
  if (scenario.operation === 'getPage') return client.getPage().invoke(call, GetPageInputSchema.parse(scenario.input));
  if (scenario.operation === 'createPage') return productionMutation(client.createPage(), call, CreatePageInputSchema.parse(scenario.input) as CreatePageInput, scenario.idempotencyKey!);
  if (scenario.operation === 'updatePageProperties') return productionMutation(client.updatePageProperties(), call, UpdatePagePropertiesInputSchema.parse(scenario.input) as UpdatePagePropertiesInput, scenario.idempotencyKey!);
  throw new Error(`unknown Notion operation ${scenario.operation}`);
}
