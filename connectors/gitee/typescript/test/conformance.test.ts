// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { GiteeClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/gitee/scenarios.json', import.meta.url));
const connection = { provider: 'gitee', name: 'gitee' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeGitee, ''));

async function invokeGitee(scenario: Scenario, recorder: ExchangeRecorder) {
  const accessToken = scenario.name.endsWith('defect') ? '' : 'test-gitee-token';
  const credentials: Array<[typeof connection, { accessToken: SecretString }]> = [[connection, { accessToken: new SecretString(accessToken) }]];
  const client = new GiteeClient({ maxResponseBytes: ['getRepository-oversized', 'getRepository-missing-smallLimit'].includes(scenario.name) ? 16 : 262144 }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'gitee-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'gitee', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getRepository') return client.getRepository().invoke(call, scenario.input as { owner: string; repo: string });
  throw new Error(`unknown Gitee operation ${scenario.operation}`);
}
