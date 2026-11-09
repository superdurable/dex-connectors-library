// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { FacebookLeadAdsClient } from '../src/client.js';
import { GetLeadInputSchema, ListLeadFormsInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/meta/facebook-lead-ads/scenarios.json', import.meta.url));
const connection = { provider: 'facebook-lead-ads', name: 'facebook-lead-ads' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeFacebookLeadAds, '/v25.0'));

async function invokeFacebookLeadAds(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { pageAccessToken: SecretString }]> = [[connection, { pageAccessToken: new SecretString('test-page-token') }]];
  const client = new FacebookLeadAdsClient({ baseUrl: 'http://127.0.0.1:12345/v25.0' }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'facebook-lead-ads-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'facebook-lead-ads', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'listLeadForms') return client.listLeadForms().invoke(call, ListLeadFormsInputSchema.parse(scenario.input));
  if (scenario.operation === 'getLead') return client.getLead().invoke(call, GetLeadInputSchema.parse(scenario.input));
  throw new Error(`unknown Facebook Lead Ads operation ${scenario.operation}`);
}
