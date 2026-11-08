// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { MetaAdsInsightsClient } from '../src/client.js';
import { GetAdInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/meta/ads-insights/scenarios.json', import.meta.url));
const connection = { provider: 'meta-ads-insights', name: 'meta-ads-insights' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeMetaAdsInsights, '/v25.0'));

async function invokeMetaAdsInsights(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { userAccessToken: SecretString }]> = [[connection, { userAccessToken: new SecretString('test-marketing-token') }]];
  const client = new MetaAdsInsightsClient({ baseUrl: 'http://127.0.0.1:12345/v25.0' }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'meta-ads-insights-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'meta-ads-insights', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getAd') return client.getAd().invoke(call, GetAdInputSchema.parse(scenario.input));
  throw new Error(`unknown Meta Ads Insights operation ${scenario.operation}`);
}
