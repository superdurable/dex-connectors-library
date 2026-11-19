// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { S3Client } from '../src/client.js';
import { GetObjectTextInputSchema, HeadObjectInputSchema, ListObjectsInputSchema, PutObjectInputSchema, type PutObjectInput, type StoredObject } from '../src/models.js';
import { answerRecordedRequest, ExchangeRecorder, invokeProductionMutation, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/amazon/s3/scenarios.json', import.meta.url));
const noProgress = { reportProgress: () => {}, writeText: () => {}, hasProgressStream: () => false, hasTextStream: () => false };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeS3));

async function invokeS3(scenario: Scenario, recorder: ExchangeRecorder, context: unknown) {
  const send = (request: ProviderRequest) => {
    return answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  };
  const client = new S3Client({ endpoint: 'http://127.0.0.1:12345', region: 'eu-west-1', defaultBucket: 'acme-reports' }, {
    resolve: async () => ({ accessKeyId: new SecretString('AKIAS3CONNECTORTEST1'), secretAccessKey: new SecretString('s3-connector-test/secret+value') }),
  }, { send, now: () => new Date('2026-10-25T09:00:00Z') });
  const call = scenarioCall(scenario, { provider: 'amazon-s3', name: 's3-test' }, 'amazon-s3', context, noProgress) as Call;
  if (scenario.operation === 'listObjects') return client.listObjects().invoke(call, ListObjectsInputSchema.parse(scenario.input));
  if (scenario.operation === 'headObject') return client.headObject().invoke(call, HeadObjectInputSchema.parse(scenario.input));
  if (scenario.operation === 'getObjectText') return client.getObjectText().invoke(call, GetObjectTextInputSchema.parse(scenario.input));
  if (scenario.operation === 'putObject') {
    const operation = client.putObject() as Mutation<PutObjectInput, StoredObject>;
    const input = PutObjectInputSchema.parse(scenario.input);
    return invokeProductionMutation(operation, scenario.idempotencyKey!,
      recording => runMutation(recording as Mutation<PutObjectInput, StoredObject>, call, input), error => error instanceof RetryError);
  }
  throw new Error(`unknown S3 operation ${scenario.operation}`);
}
