// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';
import { BitbucketClient } from '../src/client.js';
import { ListPullRequestsInputSchema, ListRepositoriesInputSchema } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/atlassian/bitbucket/scenarios.json', import.meta.url));
const connection = { provider: 'bitbucket', name: 'bitbucket' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeBitbucket));

async function invokeBitbucket(scenario: Scenario, recorder: ExchangeRecorder) {
  const sender = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const client = new BitbucketClient({ baseUrl: 'http://127.0.0.1:12345/2.0' }, {
    resolve: async () => ({ accessToken: new SecretString('test-token') }),
  }, { send: sender });
  const call = scenarioCall(scenario, connection, 'bitbucket', {
    flowId: 'bitbucket-conformance', cancellationSignal: new AbortController().signal,
  }, { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  if (scenario.operation === 'listRepositories') return client.listRepositories().invoke(call, ListRepositoriesInputSchema.parse(scenario.input));
  if (scenario.operation === 'listPullRequests') return client.listPullRequests().invoke(call, ListPullRequestsInputSchema.parse(scenario.input));
  throw new Error(`unknown Bitbucket operation ${scenario.operation}`);
}
