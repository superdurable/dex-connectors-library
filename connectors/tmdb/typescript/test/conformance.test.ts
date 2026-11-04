// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { TMDBClient } from '../src/client.js';
import { GetMovieInputSchema, SearchMoviesInputSchema } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/tmdb/scenarios.json', import.meta.url));
const connection = { provider: 'tmdb', name: 'tmdb' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeTMDB));

async function invokeTMDB(scenario: Scenario, recorder: ExchangeRecorder) {
  const send = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const client = new TMDBClient({ baseUrl: 'http://127.0.0.1:12345/3' }, {
    resolve: async () => ({ apiKey: new SecretString('test-secret-token') }),
  }, { send });
  const call = scenarioCall(scenario, connection, 'tmdb', { flowId: 'tmdb-conformance', cancellationSignal: new AbortController().signal },
    { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  if (scenario.operation === 'searchMovies') return client.searchMovies().invoke(call, SearchMoviesInputSchema.parse(scenario.input));
  if (scenario.operation === 'getMovie') return client.getMovie().invoke(call, GetMovieInputSchema.parse(scenario.input));
  throw new Error(`unknown TMDB operation ${scenario.operation}`);
}
