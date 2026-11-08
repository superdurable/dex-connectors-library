// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { AdsClient } from '../src/client.js';
import { ListAdAccountsInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/linkedin/ads/scenarios.json', import.meta.url));
const connection = { provider: 'linkedin-ads', name: 'linkedin-ads' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeAds, ''));

async function invokeAds(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials = {
    oauthClientId: 'synthetic-client-id', oauthClientSecret: new SecretString('synthetic-client-secret'),
    accessToken: new SecretString('test-linkedin-ads-token'), refreshToken: new SecretString('synthetic-refresh-token'),
  };
  const client = new AdsClient({ baseUrl: 'http://127.0.0.1:12345' }, new StaticCredentialProvider([[connection, credentials]]), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'linkedin-ads-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'linkedin-ads', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'listAdAccounts') return client.listAdAccounts().invoke(call, ListAdAccountsInputSchema.parse(scenario.input));
  throw new Error(`unknown Ads operation ${scenario.operation}`);
}
