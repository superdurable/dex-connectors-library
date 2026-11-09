// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { ClickHouseClient } from '../src/client.js';
import { ListTablesInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/clickhouse/scenarios.json', import.meta.url));
const connection = { provider: 'clickhouse', name: 'clickhouse' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeClickHouse, ''));

async function invokeClickHouse(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { username: string; password: SecretString }]> = [[connection, { username: 'reader', password: new SecretString('sample-clickhouse-password') }]];
  const client = new ClickHouseClient({ baseUrl: 'http://127.0.0.1:12345' }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'clickhouse-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'clickhouse', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'listTables') return client.listTables().invoke(call, ListTablesInputSchema.parse(scenario.input));
  throw new Error(`unknown ClickHouse operation ${scenario.operation}`);
}
