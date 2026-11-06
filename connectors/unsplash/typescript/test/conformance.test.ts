// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { answerRecordedRequest, ExchangeRecorder, loadScenarios, replay, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { Client } from '../src/client.js';
import { GetPhotoInputSchema, SearchPhotosInputSchema } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/unsplash/scenarios.json', import.meta.url));
const connection = { provider: 'unsplash', name: 'unsplash' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeUnsplash));
async function invokeUnsplash(scenario: Scenario, recorder: ExchangeRecorder) {
  {
    const client = new Client({ baseUrl: 'https://api.unsplash.com' }, new StaticCredentialProvider([[connection, { accessKey: new SecretString('unsplash-test-key') }]]), { send: (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)) });
    const call: Call = { context: { flowId: 'unsplash-conformance', cancellationSignal: new AbortController().signal } as Call['context'], id: scenario.callId, connection, operation: { connectorId: 'unsplash', operationId: scenario.operation }, reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false };
    if (scenario.operation === 'searchPhotos') return client.searchPhotos().invoke(call, SearchPhotosInputSchema.parse(scenario.input));
    if (scenario.operation === 'getPhoto') return client.getPhoto().invoke(call, GetPhotoInputSchema.parse(scenario.input));
    throw new Error(`unknown operation ${scenario.operation}`);
  }
}
