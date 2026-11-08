// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import {
  RetryError, SecretString, runMutation,
  type Call, type CredentialRefreshDriver, type Mutation,
} from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { BoxClient } from '../src/client.js';
import { DownloadFileInputSchema, UploadFileInputSchema, type Credentials } from '../src/models.js';
import {
  answerRecordedRequest, ExchangeRecorder, HeartbeatContext, invokeProductionMutation,
  loadScenarios, replay, scenarioCall, type Scenario,
} from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/box/scenarios.json', import.meta.url));
const connection = { provider: 'box', name: 'box' };

function productionMutation<Input, Output>(operation: Mutation<Input, Output>, call: Call, input: Input, key: string) {
  return invokeProductionMutation(operation, key,
    recording => runMutation(recording as Mutation<Input, Output>, call, input), error => error instanceof RetryError);
}

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeBoxAttempt));

async function invokeBoxAttempt(scenario: Scenario, recorder: ExchangeRecorder, context: HeartbeatContext) {
  const credentials: Credentials = {
    oauthClientId: 'client-id', oauthClientSecret: new SecretString('client-secret'),
    accessToken: new SecretString('test-token'), refreshToken: new SecretString('refresh-token'),
  };
  const driver: CredentialRefreshDriver<Credentials> = {
    refreshRequired: () => false,
    refresh: async () => { recorder.mismatches.push('unexpected credential refresh'); throw new Error('unexpected refresh'); },
  };
  const client = new BoxClient({
    baseUrl: 'http://127.0.0.1:12345', uploadBaseUrl: 'http://127.0.0.1:12345',
    maxResponseBytes: scenario.name === 'downloadFile-invalidResponse' ? 8 : 2_097_152,
    maxDownloadBytes: 8, maxUploadBytes: 8,
  }, { resolve: async () => credentials }, {
    refreshDriver: driver,
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call = scenarioCall(scenario, connection, 'box', context) as Call;
  if (scenario.operation === 'downloadFile') return client.downloadFile().invoke(call, DownloadFileInputSchema.parse(scenario.input));
  if (scenario.operation === 'uploadFile') return productionMutation(client.uploadFile(), call, UploadFileInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  throw new Error(`unknown operation ${scenario.operation}`);
}
