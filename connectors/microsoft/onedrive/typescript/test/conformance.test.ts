// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, StaticCredentialProvider, type Call, type CredentialRefreshDriver, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { OneDriveClient } from '../src/client.js';
import { CreateFolderInputSchema, GetFileInputSchema, ReadFileTextInputSchema, SearchFilesInputSchema, UploadFileInputSchema, type Credentials } from '../src/models.js';
import { answerRecordedRequest, ExchangeRecorder, HeartbeatContext, invokeProductionMutation, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/microsoft/onedrive/scenarios.json', import.meta.url));
const connection = { provider: 'microsoft', name: 'onedrive-files' };
const credentials: Credentials = { accessToken: new SecretString('graph-token-SENTINEL') };

function productionMutation<Input, Output>(operation: Mutation<Input, Output>, call: Call, input: Input, key: string) {
  return invokeProductionMutation(operation, key,
    recording => runMutation(recording as Mutation<Input, Output>, call, input), error => error instanceof RetryError);
}

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeOneDriveAttempt, '/v1.0'));

async function invokeOneDriveAttempt(scenario: Scenario, recorder: ExchangeRecorder, context: HeartbeatContext) {
  const driver: CredentialRefreshDriver<Credentials> = { refreshRequired: () => false,
    refresh: async () => { recorder.mismatches.push('unexpected credential refresh'); throw new Error('unexpected refresh'); } };
  const client = new OneDriveClient({ endpoint: 'http://127.0.0.1:12345' },
    new StaticCredentialProvider([[connection, credentials]]),
    { refreshDriver: driver, transport: { send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)) } });
  const call = scenarioCall(scenario, connection, 'microsoft-onedrive', context) as Call;
  if (scenario.operation === 'searchFiles') return client.searchFiles().invoke(call, SearchFilesInputSchema.parse(scenario.input));
  if (scenario.operation === 'getFile') return client.getFile().invoke(call, GetFileInputSchema.parse(scenario.input));
  if (scenario.operation === 'readFileText') return client.readFileText().invoke(call, ReadFileTextInputSchema.parse(scenario.input));
  if (scenario.operation === 'uploadFile') return productionMutation(client.uploadFile(), call, UploadFileInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'createFolder') return productionMutation(client.createFolder(), call, CreateFolderInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  throw new Error(`unknown operation ${scenario.operation}`);
}
