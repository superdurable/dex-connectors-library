// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, StaticCredentialProvider, type Call, type CredentialRefreshDriver, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { DriveClient } from '../src/client.js';
import type { Credentials } from '../src/generated/models.js';
import { answerRecordedRequest, ExchangeRecorder, HeartbeatContext, invokeProductionMutation, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/google/drive/scenarios.json', import.meta.url));
const connection = { provider: 'google', name: 'drive-files' };
const credentials: Credentials = { accessToken: new SecretString('drive-token-SENTINEL') };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeDriveAttempt));

async function invokeDriveAttempt(scenario: Scenario, recorder: ExchangeRecorder, context: HeartbeatContext) {
  const driver: CredentialRefreshDriver<Credentials> = { refreshRequired: () => false,
    refresh: async () => { recorder.mismatches.push('unexpected credential refresh'); throw new Error('unexpected refresh'); } };
  const transport = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const client = new DriveClient({ endpoint: 'http://localhost', maxTextBytes: 8 },
    new StaticCredentialProvider([[connection, credentials]]), { refreshDriver: driver, transport, now: () => new Date('2026-03-04T09:15:00Z') });
  const call = scenarioCall(scenario, connection, 'google-drive', context) as Call;
  if (scenario.operation === 'searchFiles') return client.searchFiles().invoke(call, scenario.input);
  if (scenario.operation === 'getFile') return client.getFile().invoke(call, scenario.input as { fileId: string });
  if (scenario.operation === 'readFileText') return client.readFileText().invoke(call, scenario.input as { fileId: string });
  if (scenario.operation === 'uploadFile') return invokeProductionMutation(client.uploadFile(), scenario.idempotencyKey!,
    recording => runMutation(recording as Mutation<{ name: string; mimeType: string }, unknown>, call, scenario.input as { name: string; mimeType: string }), error => error instanceof RetryError);
  throw new Error(`unknown operation ${scenario.operation}`);
}
