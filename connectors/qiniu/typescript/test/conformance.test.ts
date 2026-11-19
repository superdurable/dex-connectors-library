// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { QiniuClient } from '../src/client.js';
import type { ListBucketsInput, Credentials } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/qiniu/scenarios.json', import.meta.url));
const connection = { provider: 'qiniu', name: 'bucket-list' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeQiniu));

async function invokeQiniu(scenario: Scenario, recorder: ExchangeRecorder) {
  const send = async (request: ProviderRequest) => {
    const response = await answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
    if (response.body.length > request.maxResponseBytes!) throw new BodyTooLargeError(response.status, response.headers);
    return response;
  };
  const credentials: Credentials = { accessKey: new SecretString('test-access-key'), secretKey: new SecretString('test-secret-key') };
  const source = new StaticCredentialProvider(scenario.name === 'listBuckets-defect' ? [] : [[connection, credentials]]);
  const config = { baseUrl: 'http://127.0.0.1:12345', ...(scenario.name === 'listBuckets-oversized' ? { maxResponseBytes: 16 } : {}) };
  const client = new QiniuClient(config, source, { send, now: () => new Date('2026-11-14T00:30:05Z') });
  const call = scenarioCall(scenario, connection, 'qiniu', {
    flowId: 'qiniu-conformance', cancellationSignal: new AbortController().signal,
  }, { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  if (scenario.operation !== 'listBuckets') throw new Error(`unknown Qiniu operation ${scenario.operation}`);
  return client.listBuckets().invoke(call, scenario.input as unknown as ListBucketsInput);
}
