// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { MicrosoftAdvertisingClient } from '../src/client.js';
import { ListCampaignsInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/microsoft/advertising/scenarios.json', import.meta.url));
const connection = { provider: 'microsoft-advertising', name: 'microsoft-advertising' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeMicrosoftAdvertising, ''));

async function invokeMicrosoftAdvertising(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials = {
    oauthClientId: 'synthetic-client-id', oauthClientSecret: new SecretString('synthetic-client-secret'),
    developerToken: new SecretString('test-developer-token'),
    accessToken: new SecretString('test-microsoft-advertising-token'), refreshToken: new SecretString('synthetic-refresh-token'),
  };
  const client = new MicrosoftAdvertisingClient({
    baseUrl: 'http://127.0.0.1:12345', customerId: '73456',
  }, new StaticCredentialProvider([[connection, credentials]]), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'microsoft-advertising-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'microsoft-advertising', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'listCampaigns') return client.listCampaigns().invoke(call, ListCampaignsInputSchema.parse(scenario.input));
  throw new Error(`unknown MicrosoftAdvertising operation ${scenario.operation}`);
}
