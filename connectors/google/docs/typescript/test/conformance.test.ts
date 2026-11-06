// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, StaticCredentialProvider, type Call, type CredentialRefreshDriver, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { DocsClient } from '../src/client.js';
import { AppendTextInputSchema, CreateDocumentInputSchema, GetDocumentTextInputSchema, ReplaceDocumentTextInputSchema, type Credentials } from '../src/generated/models.js';
import { answerRecordedRequest, ExchangeRecorder, HeartbeatContext, invokeProductionMutation, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/google/docs/scenarios.json', import.meta.url));
const connection = { provider: 'google', name: 'policy-docs' };
const credentials: Credentials = { accessToken: new SecretString('docs-token-SENTINEL') };

function productionMutation<Input, Output>(operation: Mutation<Input, Output>, call: Call, input: Input, key: string) {
  return invokeProductionMutation(operation, key,
    recording => runMutation(recording as Mutation<Input, Output>, call, input), error => error instanceof RetryError);
}

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeDocsAttempt));

async function invokeDocsAttempt(scenario: Scenario, recorder: ExchangeRecorder, context: HeartbeatContext) {
  const driver: CredentialRefreshDriver<Credentials> = { refreshRequired: () => false,
    refresh: async () => { recorder.mismatches.push('unexpected credential refresh'); throw new Error('unexpected refresh'); } };
  const transport = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const config = { docsEndpoint: 'http://127.0.0.1:12345', driveEndpoint: 'http://127.0.0.1:12345',
    ...(scenario.name.includes('response-too-large') ? { maxResponseBytes: 128 } : {}),
    ...(scenario.name.includes('text-too-large') ? { maxTextBytes: 2 } : {}) };
  const client = new DocsClient(config, new StaticCredentialProvider([[connection, credentials]]),
    { refreshDriver: driver, transport, now: () => new Date('2026-10-01T09:00:00Z') });
  const call = scenarioCall(scenario, connection, 'google-docs', context) as Call;
  if (scenario.operation === 'getDocumentText') return client.getDocumentText().invoke(call, GetDocumentTextInputSchema.parse(scenario.input));
  if (scenario.operation === 'createDocument') return productionMutation(client.createDocument(), call, CreateDocumentInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'replaceDocumentText') return productionMutation(client.replaceDocumentText(), call, ReplaceDocumentTextInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'appendText') return productionMutation(client.appendText(), call, AppendTextInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  throw new Error(`unknown operation ${scenario.operation}`);
}
