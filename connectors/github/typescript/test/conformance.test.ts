// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { GitHubClient } from '../src/client.js';
import { GetAuthenticatedProfileInputSchema, ListPublicRepositoriesInputSchema, ListMergedPullRequestsInputSchema, ListPullRequestFilesInputSchema, ListReleasesInputSchema, ListCommitsInputSchema } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/github/scenarios.json', import.meta.url));
const connection = { provider: 'github', name: 'signup' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeGitHub));

async function invokeGitHub(scenario: Scenario, recorder: ExchangeRecorder) {
  const sender = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const credentials = new StaticCredentialProvider([[connection, { accessToken: new SecretString(scenario.name === 'getAuthenticatedProfile-defect' ? '' : 'one-use-token'), oauthClientId: 'client-id', oauthClientSecret: new SecretString('client-secret') }]]);
  const client = new GitHubClient({ baseUrl: 'http://127.0.0.1:12345' }, credentials, { send: sender });
  const call = scenarioCall(scenario, connection, 'github', {
    flowId: 'github-conformance', cancellationSignal: new AbortController().signal,
  }, { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  switch (scenario.operation) {
    case 'getAuthenticatedProfile': return client.getAuthenticatedProfile().invoke(call, GetAuthenticatedProfileInputSchema.parse(scenario.input));
    case 'listPublicRepositories': return client.listPublicRepositories().invoke(call, ListPublicRepositoriesInputSchema.parse(scenario.input));
    case 'listMergedPullRequests': return client.listMergedPullRequests().invoke(call, ListMergedPullRequestsInputSchema.parse(scenario.input));
    case 'listPullRequestFiles': return client.listPullRequestFiles().invoke(call, ListPullRequestFilesInputSchema.parse(scenario.input));
    case 'listReleases': return client.listReleases().invoke(call, ListReleasesInputSchema.parse(scenario.input));
    case 'listCommits': return client.listCommits().invoke(call, ListCommitsInputSchema.parse(scenario.input));
    default: throw new TypeError(`unknown GitHub operation ${scenario.operation}`);
  }
}
