// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { AzureDevOpsClient } from '../src/client.js';
import { GetWorkItemInputSchema, ListProjectsInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/microsoft/azure-devops/scenarios.json', import.meta.url));
const connection = { provider: 'azure-devops', name: 'azure-devops' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeAzureDevOps, '/example'));

async function invokeAzureDevOps(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { personalAccessToken: SecretString }]> = scenario.name === 'listProjects-defect' ? [] : [[connection, { personalAccessToken: new SecretString('test-token') }]];
  const client = new AzureDevOpsClient({ baseUrl: 'http://127.0.0.1:12345/example' }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'azure-devops-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'azure-devops', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'listProjects') return client.listProjects().invoke(call, ListProjectsInputSchema.parse(scenario.input));
  if (scenario.operation === 'getWorkItem') return client.getWorkItem().invoke(call, GetWorkItemInputSchema.parse(scenario.input));
  throw new Error(`unknown AzureDevOps operation ${scenario.operation}`);
}
