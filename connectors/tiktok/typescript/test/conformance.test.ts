// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { TikTokClient } from '../src/client.js';
import { ListCampaignsInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/tiktok/scenarios.json', import.meta.url));
const connection = { provider: 'tiktok-business', name: 'tiktok-business' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeTikTok, '/open_api/v1.3'));

async function invokeTikTok(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { accessToken: SecretString }]> = [[connection, { accessToken: new SecretString('sample-tiktok-token') }]];
  const client = new TikTokClient({ baseUrl: 'http://127.0.0.1:12345/open_api/v1.3' }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'tiktok-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'tiktok-business', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'listCampaigns') return client.listCampaigns().invoke(call, ListCampaignsInputSchema.parse(scenario.input));
  throw new Error(`unknown TikTok operation ${scenario.operation}`);
}
