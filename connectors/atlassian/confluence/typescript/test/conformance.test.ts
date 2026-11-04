// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { RetryError, runMutation, SecretString, StaticCredentialProvider, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ConfluenceClient } from '../src/client.js';
import { answerRecordedRequest, ExchangeRecorder, invokeProductionMutation, loadScenarios, replay, scenarioCall, type HeartbeatContext, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';
import { SearchPagesInputSchema, GetPageInputSchema, CreatePageInputSchema, UpdatePageInputSchema, AddCommentInputSchema } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/atlassian/confluence/scenarios.json', import.meta.url));
const connection = { provider: 'atlassian', name: 'confluence-test' };
const cloudId = '11223344-a1b2-4b33-8c44-def123456789';

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeConfluence));

async function invokeConfluence(scenario: Scenario, recorder: ExchangeRecorder, context: HeartbeatContext) {
  Object.assign(context, { firstAttemptAt: new Date('2026-10-25T09:00:00Z') });
  const sender = (request: ProviderRequest) => answerRecordedRequest(recorder, request, beforeSend => new ProviderTransportError(beforeSend));
  const credentials = new StaticCredentialProvider([[connection, {
    oauthClientId: 'confluence-test-client', oauthClientSecret: new SecretString('confluence-test-client-secret'),
    accessToken: new SecretString('confluence-access-token'), refreshToken: new SecretString('confluence-test-refresh-token'),
  }]]);
  const client = new ConfluenceClient({ endpoint: 'http://127.0.0.1:12345', cloudId }, credentials, { send: sender, now: () => new Date('2026-11-04T00:00:00Z') });
  const call = scenarioCall(scenario, connection, 'confluence', context, {
    reportProgress: () => {}, writeText: () => {}, hasProgressStream: () => false, hasTextStream: () => false,
  }) as Call;
  if (scenario.operation === 'searchPages') return client.searchPages().invoke(call, SearchPagesInputSchema.parse(scenario.input));
  if (scenario.operation === 'getPage') return client.getPage().invoke(call, GetPageInputSchema.parse(scenario.input));
  if (scenario.operation === 'createPage') return productionMutation(client.createPage(), call, CreatePageInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'updatePage') return productionMutation(client.updatePage(), call, UpdatePageInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'addComment') return productionMutation(client.addComment(), call, AddCommentInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  throw new TypeError(`unknown operation ${scenario.operation}`);
}
function productionMutation<Input, Output>(operation: Mutation<Input, Output>, call: Call, input: Input, key: string) {
  return invokeProductionMutation(operation, key,
    recording => runMutation(recording as Mutation<Input, Output>, call, input), error => error instanceof RetryError);
}
