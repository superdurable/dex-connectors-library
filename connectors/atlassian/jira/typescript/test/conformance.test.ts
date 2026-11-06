// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, invokeProductionMutation, loadScenarios, replay, scenarioCall, type HeartbeatContext, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';
import { JiraClient } from '../src/client.js';
import { AddCommentInputSchema, CreateIssueInputSchema, GetIssueInputSchema, SearchIssuesInputSchema, TransitionIssueInputSchema } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/atlassian/jira/scenarios.json', import.meta.url));
const connection = { provider: 'atlassian', name: 'jira-test' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeJira, '/ex/jira/11223344-a1b2-4b33-8c44-def123456789/rest/api/3'));

function productionMutation<Input, Output>(operation: Mutation<Input, Output>, call: Call, input: Input, key: string) {
  return invokeProductionMutation(operation, key, recording => runMutation(recording as Mutation<Input, Output>, call, input), error => error instanceof RetryError);
}
async function invokeJira(scenario: Scenario, recorder: ExchangeRecorder, context: HeartbeatContext) {
  const send = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const credentials = { oauthClientId: 'client-id', oauthClientSecret: new SecretString('client-secret'), accessToken: new SecretString('jira-access-token'), refreshToken: new SecretString('refresh-token') };
  const client = new JiraClient({ endpoint: 'http://127.0.0.1', cloudId: '11223344-a1b2-4b33-8c44-def123456789' }, { resolve: async () => credentials, resolveWithRefresh: async () => credentials }, { send });
  const call = scenarioCall(scenario, connection, 'jira', context, { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  if (scenario.operation === 'searchIssues') return client.searchIssues().invoke(call, SearchIssuesInputSchema.parse(scenario.input));
  if (scenario.operation === 'getIssue') return client.getIssue().invoke(call, GetIssueInputSchema.parse(scenario.input));
  if (scenario.operation === 'createIssue') return productionMutation(client.createIssue(), call, CreateIssueInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'transitionIssue') return productionMutation(client.transitionIssue(), call, TransitionIssueInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'addComment') return productionMutation(client.addComment(), call, AddCommentInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  throw new Error(`unknown Jira operation ${scenario.operation}`);
}
