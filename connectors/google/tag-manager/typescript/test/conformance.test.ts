// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { TagManagerClient } from '../src/client.js';
import { ListAccountsInputSchema, ListContainersInputSchema, ListTagsInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/google/tag-manager/scenarios.json', import.meta.url));
const connection = { provider: 'google-tag-manager', name: 'google-tag-manager' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeTagManager));

async function invokeTagManager(scenario: Scenario, recorder: ExchangeRecorder) {
  const token = scenario.name === 'listContainers-defect' ? '' : 'test-tag-manager-token';
  const credentials: Array<[typeof connection, { oauthClientId: string; oauthClientSecret: SecretString; accessToken: SecretString; refreshToken: SecretString }]> = [[connection, {
    oauthClientId: 'test-client', oauthClientSecret: new SecretString('test-client-secret'),
    accessToken: new SecretString(token), refreshToken: new SecretString('test-refresh-token'),
  }]];
  const client = new TagManagerClient({ baseUrl: 'http://127.0.0.1:12345' }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'google-tag-manager-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'google-tag-manager', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  switch (scenario.operation) {
    case 'listAccounts': return client.listAccounts().invoke(call, ListAccountsInputSchema.parse(scenario.input));
    case 'listContainers': return client.listContainers().invoke(call, ListContainersInputSchema.parse(scenario.input));
    case 'listTags': return client.listTags().invoke(call, ListTagsInputSchema.parse(scenario.input));
    default: throw new Error(`unsupported Tag Manager operation ${scenario.operation}`);
  }
}
