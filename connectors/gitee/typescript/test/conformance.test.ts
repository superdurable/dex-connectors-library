// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { GiteeClient } from '../src/client.js';
import type { GetRepositoryInput, Credentials } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/gitee/scenarios.json', import.meta.url));
const connection = { provider: 'gitee', name: 'repository-lookup' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeGitee));

async function invokeGitee(scenario: Scenario, recorder: ExchangeRecorder) {
  const send = async (request: ProviderRequest) => {
    const response = await answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
    if (response.body.length > request.maxResponseBytes!) throw new BodyTooLargeError(response.status, response.headers);
    return response;
  };
  const credentials: Credentials = { accessToken: new SecretString('test-gitee-token') };
  const source = new StaticCredentialProvider(scenario.name === 'getRepository-defect' ? [] : [[connection, credentials]]);
  const config = { baseUrl: 'http://127.0.0.1', ...(scenario.name === 'getRepository-oversized' ? { maxResponseBytes: 16 } : {}) };
  const client = new GiteeClient(config, source, { send });
  const call = scenarioCall(scenario, connection, 'gitee', {
    flowId: 'gitee-conformance', cancellationSignal: new AbortController().signal,
  }, { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  if (scenario.operation !== 'getRepository') throw new Error(`unknown Gitee operation ${scenario.operation}`);
  return client.getRepository().invoke(call, scenario.input as unknown as GetRepositoryInput);
}
