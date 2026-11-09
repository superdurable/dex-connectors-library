// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { SearchConsoleClient } from '../src/client.js';
import { ListSitesInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/google/search-console/scenarios.json', import.meta.url));
const connection = { provider: 'google-search-console', name: 'google-search-console' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeSearchConsole));

async function invokeSearchConsole(scenario: Scenario, recorder: ExchangeRecorder) {
  const token = scenario.name === 'listSites-defect' ? '' : 'test-search-console-token';
  const credentials: Array<[typeof connection, { oauthClientId: string; oauthClientSecret: SecretString; accessToken: SecretString; refreshToken: SecretString }]> = [[connection, {
    oauthClientId: 'test-client', oauthClientSecret: new SecretString('test-client-secret'),
    accessToken: new SecretString(token), refreshToken: new SecretString('test-refresh-token'),
  }]];
  const client = new SearchConsoleClient({ baseUrl: 'http://127.0.0.1:12345' }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'search-console-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'google-search-console', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.listSites().invoke(call, ListSitesInputSchema.parse(scenario.input));
}
