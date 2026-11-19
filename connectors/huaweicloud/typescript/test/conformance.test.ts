// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { HuaweiCloudClient } from '../src/client.js';
import type { ListAccessibleProjectsInput, Credentials } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/huaweicloud/scenarios.json', import.meta.url));
const connection = { provider: 'huaweicloud', name: 'project-list' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeHuaweiCloud));

async function invokeHuaweiCloud(scenario: Scenario, recorder: ExchangeRecorder) {
  const send = async (request: ProviderRequest) => {
    const response = await answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
    if (response.body.length > request.maxResponseBytes!) throw new BodyTooLargeError(response.status, response.headers);
    return response;
  };
  const credentials: Credentials = {
    accountName: new SecretString('example-account'),
    iamUsername: new SecretString('example-user'),
    iamPassword: new SecretString('synthetic-test-password'),
  };
  const source = new StaticCredentialProvider(scenario.name === 'listAccessibleProjects-defect' ? [] : [[connection, credentials]]);
  const config = scenario.name === 'listAccessibleProjects-oversizedAuth' ? { maxResponseBytes: 16 } : {};
  const client = new HuaweiCloudClient(config, source, { send });
  const call = scenarioCall(scenario, connection, 'huaweicloud', {
    flowId: 'huaweicloud-conformance', cancellationSignal: new AbortController().signal,
  }, { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  if (scenario.operation !== 'listAccessibleProjects') throw new Error(`unknown HuaweiCloud operation ${scenario.operation}`);
  return client.listAccessibleProjects().invoke(call, scenario.input as unknown as ListAccessibleProjectsInput);
}
