// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { SpotifyClient } from '../src/client.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/spotify/scenarios.json', import.meta.url));
const connection = { provider: 'spotify', name: 'playlist-reader' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeSpotify));

async function invokeSpotify(scenario: Scenario, recorder: ExchangeRecorder) {
  const send = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const credentials = new StaticCredentialProvider([[connection, { accessToken: new SecretString('one-use-token') }]]);
  const client = new SpotifyClient({ endpoint: 'http://127.0.0.1:37801/v1' }, credentials, { send });
  const call = scenarioCall(scenario, connection, 'spotify', {
    flowId: 'spotify-conformance', cancellationSignal: new AbortController().signal,
  }, { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  if (scenario.operation !== 'listPlaylistTracks') throw new Error(`unknown Spotify operation ${scenario.operation}`);
  return client.listPlaylistTracks().invoke(call, scenario.input as { playlistId: string });
}
