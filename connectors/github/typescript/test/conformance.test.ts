// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { GitHubClient } from '../src/client.js';
import type { Credentials, GetAuthenticatedProfileInput, ListPublicRepositoriesInput, ListMergedPullRequestsInput, ListPullRequestFilesInput, ListReleasesInput, ListCommitsInput } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/github/scenarios.json', import.meta.url));
const connection = { provider: 'github', name: 'signup' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeGitHub));

async function invokeGitHub(scenario: Scenario, recorder: ExchangeRecorder) {
  const sender = async (request: ProviderRequest) => {
    const response = await answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
    if (response.body.length > request.maxResponseBytes!) throw new BodyTooLargeError(response.status, response.headers);
    return response;
  };
  const accessToken = scenario.name === 'getAuthenticatedProfile-defect' ? '' : scenario.name === 'getAuthenticatedProfile-refresh-after-rejection' ? 'rejected-token' : 'manifest-access-token';
  const initialCredentials: Credentials = { accessToken: new SecretString(accessToken), oauthClientId: 'client-id', oauthClientSecret: new SecretString('client-secret') };
  const credentials = new StaticCredentialProvider([[connection, initialCredentials]]);
  const credentialSource = scenario.name === 'getAuthenticatedProfile-refresh-after-rejection'
    ? { resolve: (call: Call) => credentials.resolve(call), resolveWithRefresh: (call: Call) => credentials.resolveWithRefresh(call),
        resolveAfterRejection: async () => ({ ...initialCredentials, accessToken: new SecretString('replacement-token') }) }
    : credentials;
  const client = new GitHubClient({ baseUrl: 'http://127.0.0.1:12345', ...(scenario.name.includes('-oversized') ? { maxResponseBytes: 100 } : {}) }, credentialSource, { send: sender, now: () => new Date('2026-01-02T03:04:05Z') });
  const call = scenarioCall(scenario, connection, 'github', {
    flowId: 'github-conformance', cancellationSignal: new AbortController().signal,
  }, { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  // Invoke directly so malformed Go fixtures reach the connector's own validation.
  switch (scenario.operation) {
    case 'getAuthenticatedProfile': return client.getAuthenticatedProfile().invoke(call, scenario.input as GetAuthenticatedProfileInput);
    case 'listPublicRepositories': return client.listPublicRepositories().invoke(call, scenario.input as unknown as ListPublicRepositoriesInput);
    case 'listMergedPullRequests': return client.listMergedPullRequests().invoke(call, scenario.input as unknown as ListMergedPullRequestsInput);
    case 'listPullRequestFiles': return client.listPullRequestFiles().invoke(call, scenario.input as unknown as ListPullRequestFilesInput);
    case 'listReleases': return client.listReleases().invoke(call, scenario.input as unknown as ListReleasesInput);
    case 'listCommits': return client.listCommits().invoke(call, scenario.input as unknown as ListCommitsInput);
    default: throw new TypeError(`unknown GitHub operation ${scenario.operation}`);
  }
}
