// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { WorktileClient } from '../src/client.js';
import { ListProjectsInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/worktile/scenarios.json', import.meta.url));
const connection = { provider: 'worktile', name: 'worktile' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeWorktile));

async function invokeWorktile(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'listProjects') throw new Error(`unknown Worktile operation ${scenario.operation}`);
  const credentials = {
    clientId: new SecretString(scenario.name.endsWith('-defect') ? '' : 'REDACTED_WORKTILE_CLIENT_ID'),
    clientSecret: new SecretString('REDACTED_WORKTILE_CLIENT_SECRET'),
  };
  const client = new WorktileClient(scenario.name.endsWith('-oversized') ? { maxResponseBytes: 16 } : {}, new StaticCredentialProvider([[connection, credentials]]), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'worktile-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'worktile', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.listProjects().invoke(call, ListProjectsInputSchema.parse(scenario.input));
}
