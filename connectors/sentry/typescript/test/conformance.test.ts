// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type Mutation, type MutationAttempt } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, invokeProductionMutation, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { SentryClient } from '../src/client.js';
import { GetIssueInputSchema, ListIssuesInputSchema, UpdateIssueStatusInputSchema, type UpdateIssueStatusInput } from '../src/generated/models.js';
import type { Issue } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/sentry/scenarios.json', import.meta.url));
const connection = { provider: 'sentry', name: 'sentry-issue-review' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeSentry));

async function invokeSentry(scenario: Scenario, recorder: ExchangeRecorder) {
  const sender = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const client = new SentryClient({ baseUrl: 'http://127.0.0.1:12345' }, {
    resolve: async () => ({ apiKey: new SecretString('test-secret-token') }),
  }, { send: sender });
  const call = scenarioCall(scenario, connection, 'sentry', {
    flowId: 'sentry-conformance', cancellationSignal: new AbortController().signal,
  }, { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  if (scenario.operation === 'listIssues') return client.listIssues().invoke(call, ListIssuesInputSchema.parse(scenario.input));
  if (scenario.operation === 'getIssue') return client.getIssue().invoke(call, GetIssueInputSchema.parse(scenario.input));
  if (scenario.operation === 'updateIssueStatus') {
    const operation = client.updateIssueStatus();
    const input = UpdateIssueStatusInputSchema.parse(scenario.input);
    return invokeProductionMutation<UpdateIssueStatusInput, MutationAttempt<Issue>, Call>(operation, scenario.idempotencyKey!,
      recording => runMutation(recording as Mutation<UpdateIssueStatusInput, Issue>, call, input), error => error instanceof RetryError);
  }
  throw new Error(`unknown Sentry operation ${scenario.operation}`);
}
