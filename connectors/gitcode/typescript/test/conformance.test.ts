// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { GitcodeClient } from '../src/client.js';
import { GetRepositoryInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/gitcode/scenarios.json', import.meta.url));
const connection = { provider: 'gitcode', name: 'gitcode' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeGitcode, ''));

async function invokeGitcode(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { accessToken: SecretString }]> = [[connection, { accessToken: new SecretString('test-personal-access-token') }]];
  const client = new GitcodeClient({ maxResponseBytes: scenario.name.endsWith('oversized') ? 8 : 262144 }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'gitcode-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'gitcode', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getRepository') return client.getRepository().invoke(call, GetRepositoryInputSchema.parse(scenario.input));
  throw new Error(`unknown Gitcode operation ${scenario.operation}`);
}
