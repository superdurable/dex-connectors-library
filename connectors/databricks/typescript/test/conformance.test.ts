// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { DatabricksClient } from '../src/client.js';
import { GetJobRunInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/databricks/scenarios.json', import.meta.url));
const connection = { provider: 'databricks', name: 'databricks' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeDatabricks, '/api'));

async function invokeDatabricks(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { apiToken: SecretString }]> = [[connection, { apiToken: new SecretString('sample-databricks-api-token') }]];
  const client = new DatabricksClient({ baseUrl: 'http://127.0.0.1:12345/api' }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'databricks-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'databricks', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getJobRun') return client.getJobRun().invoke(call, GetJobRunInputSchema.parse(scenario.input));
  throw new Error(`unknown Databricks operation ${scenario.operation}`);
}
