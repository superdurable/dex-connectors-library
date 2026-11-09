// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, StaticCredentialProvider, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, invokeProductionMutation, loadScenarios, replay, scenarioCall, type ExchangeRecorder, type HeartbeatContext, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';
import { CloudStorageClient } from '../src/client.js';
import { UploadObjectInputSchema, DownloadObjectInputSchema, ListObjectsInputSchema } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/google/cloud-storage/scenarios.json', import.meta.url));
const connection = { provider: 'google-cloud-storage', name: 'google-cloud-storage' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeStorage));

function productionMutation<Input, Output>(operation: Mutation<Input, Output>, call: Call, input: Input, key: string) {
  return invokeProductionMutation(operation, key,
    recording => runMutation(recording as Mutation<Input, Output>, call, input), error => error instanceof RetryError);
}

async function invokeStorage(scenario: Scenario, recorder: ExchangeRecorder, context: HeartbeatContext) {
  const credentials = [[connection, {
    oauthClientId: 'test-client', oauthClientSecret: new SecretString('test-client-secret'),
    accessToken: new SecretString('test-storage-token'), refreshToken: new SecretString('test-refresh-token'),
  }]] as const;
  const send = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const client = new CloudStorageClient({ baseUrl: 'http://127.0.0.1:12345' }, new StaticCredentialProvider(credentials), { send });
  const call = scenarioCall(scenario, connection, 'google-cloud-storage', context,
    { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  switch (scenario.operation) {
    case 'listObjects': return client.listObjects().invoke(call, ListObjectsInputSchema.parse(scenario.input));
    case 'downloadObject': return client.downloadObject().invoke(call, DownloadObjectInputSchema.parse(scenario.input));
    case 'uploadObject': return productionMutation(client.uploadObject(), call, UploadObjectInputSchema.parse(scenario.input), scenario.idempotencyKey!);
    default: throw new Error(`unsupported Storage operation ${scenario.operation}`);
  }
}
