// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, invokeProductionMutation, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { GitLabClient } from '../src/client.js';
import { GetProjectInputSchema, ListIssuesInputSchema, UpdateIssueLabelsInputSchema, type UpdateIssueLabelsInput } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/gitlab/scenarios.json', import.meta.url));
const connection = { provider: 'gitlab', name: 'gitlab' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeGitLab));

function productionMutation<Input, Output>(operation: Mutation<Input, Output>, call: Call, input: Input, key: string) {
  return invokeProductionMutation(operation, key, recording => runMutation(recording as Mutation<Input, Output>, call, input), error => error instanceof RetryError);
}

async function invokeGitLab(scenario: Scenario, recorder: ExchangeRecorder) {
  const sender = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const client = new GitLabClient({ baseUrl: 'http://127.0.0.1' }, {
    resolve: async () => ({ accessToken: new SecretString('test-accesstoken') }),
  }, { send: sender });
  const call = scenarioCall(scenario, connection, 'gitlab', {
    flowId: 'gitlab-conformance', cancellationSignal: new AbortController().signal,
  }, { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  if (scenario.operation === 'getProject') return client.getProject().invoke(call, GetProjectInputSchema.parse(scenario.input));
  if (scenario.operation === 'listIssues') return client.listIssues().invoke(call, ListIssuesInputSchema.parse(scenario.input));
  if (scenario.operation === 'updateIssueLabels') return productionMutation(client.updateIssueLabels(), call, UpdateIssueLabelsInputSchema.parse(scenario.input) as UpdateIssueLabelsInput, scenario.idempotencyKey!);
  throw new Error(`unknown GitLab operation ${scenario.operation}`);
}
