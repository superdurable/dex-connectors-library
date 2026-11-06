// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, StaticCredentialProvider, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, invokeProductionMutation, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';
import { YouTubeClient } from '../src/client.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/google/youtube/scenarios.json', import.meta.url));
const connection = { provider: 'google', name: 'youtube' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeYouTube));

async function invokeYouTube(scenario: Scenario, recorder: ExchangeRecorder) {
  const send = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const credentials = new StaticCredentialProvider([[connection, { authMethodId: 'google-oauth-manage', accessToken: new SecretString('one-use-token'), oauthClientId: 'client.apps.googleusercontent.com', oauthClientSecret: new SecretString('secret'), refreshToken: new SecretString('refresh') }]]);
  const client = new YouTubeClient({ endpoint: 'http://127.0.0.1:42501/youtube/v3' }, credentials, { send });
  const call = scenarioCall(scenario, connection, 'google-youtube', { flowId: 'youtube-conformance', cancellationSignal: new AbortController().signal }, { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  switch (scenario.operation) {
    case 'searchVideos': return client.searchVideos().invoke(call, scenario.input as never);
    case 'getVideo': return client.getVideo().invoke(call, scenario.input as never);
    case 'getChannel': return client.getChannel().invoke(call, scenario.input as never);
    case 'listPlaylistItems': return client.listPlaylistItems().invoke(call, scenario.input as never);
    case 'addPlaylistItem': return invokeProductionMutation(client.addPlaylistItem(), scenario.idempotencyKey!,
      recording => runMutation(recording as Mutation<{ playlistId: string; videoId: string }, unknown>, call,
        scenario.input as { playlistId: string; videoId: string }), error => error instanceof RetryError);
    default: throw new Error(`unknown YouTube operation ${scenario.operation}`);
  }
}
