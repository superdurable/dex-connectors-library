// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { MicrosoftAzureBlobClient } from '../src/client.js';
import { GetBlobInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/microsoft/azure-blob/scenarios.json', import.meta.url));
const connection = { provider: 'microsoft-azure-blob', name: 'microsoft-azure-blob' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeMicrosoftAzureBlob, ''));

async function invokeMicrosoftAzureBlob(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { accountKey: SecretString }]> = [[connection, { accountKey: new SecretString('c2VjcmV0LXRlc3Qta2V5') }]];
  const client = new MicrosoftAzureBlobClient({ endpoint: 'http://127.0.0.1:12345', accountName: 'exampleacct' }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
    now: () => new Date('2026-10-30T00:00:00Z'),
  });
  const call: Call = {
    context: { flowId: 'azureblob-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'microsoft-azure-blob', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getBlob') return client.getBlob().invoke(call, GetBlobInputSchema.parse(scenario.input));
  throw new Error(`unknown Azure Blob Storage operation ${scenario.operation}`);
}
