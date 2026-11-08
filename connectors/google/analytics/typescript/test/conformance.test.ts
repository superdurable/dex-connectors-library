// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { Analytics4Client } from '../src/client.js';
import { GetPropertyInputSchema, ListAudiencesInputSchema, RunReportInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/google/analytics/scenarios.json', import.meta.url));
const connection = { provider: 'google-analytics-4', name: 'google-analytics-4' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeAnalytics4));

async function invokeAnalytics4(scenario: Scenario, recorder: ExchangeRecorder) {
  const token = scenario.name === 'getProperty-defect' ? '' : 'test-analytics-token';
  const credentials: Array<[typeof connection, { oauthClientId: string; oauthClientSecret: SecretString; accessToken: SecretString; refreshToken: SecretString }]> = [[connection, {
    oauthClientId: 'test-client', oauthClientSecret: new SecretString('test-client-secret'),
    accessToken: new SecretString(token), refreshToken: new SecretString('test-refresh-token'),
  }]];
  const client = new Analytics4Client({ dataBaseUrl: 'http://127.0.0.1:12345', adminBaseUrl: 'http://127.0.0.1:12345' }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'google-analytics-4-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'google-analytics-4', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  switch (scenario.operation) {
    case 'runReport': return client.runReport().invoke(call, RunReportInputSchema.parse(scenario.input));
    case 'getProperty': return client.getProperty().invoke(call, GetPropertyInputSchema.parse(scenario.input));
    case 'listAudiences': return client.listAudiences().invoke(call, ListAudiencesInputSchema.parse(scenario.input));
    default: throw new Error(`unsupported Analytics 4 operation ${scenario.operation}`);
  }
}
