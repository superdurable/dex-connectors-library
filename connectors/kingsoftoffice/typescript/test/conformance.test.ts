// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { KingsoftOfficeClient } from '../src/client.js';
import { ListFolderFilesInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/kingsoftoffice/scenarios.json', import.meta.url));
const connection = { provider: 'kingsoftoffice', name: 'kingsoftoffice' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeKingsoftOffice, ''));

async function invokeKingsoftOffice(scenario: Scenario, recorder: ExchangeRecorder) {
  const appID = scenario.name.endsWith('invalid-credentials') ? 'bad id' : 'AK123456';
  const credentials: Array<[typeof connection, { appId: string; appKey: SecretString }]> = [[connection, { appId: appID, appKey: new SecretString('sk098765') }]];
  const client = new KingsoftOfficeClient({ baseUrl: 'http://127.0.0.1:12345', maxResponseBytes: scenario.name.endsWith('oversize') ? 5 : 1048576 }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'kingsoftoffice-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'kingsoftoffice', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'listFolderFiles') return client.listFolderFiles().invoke(call, ListFolderFilesInputSchema.parse(scenario.input));
  throw new Error(`unknown WPS operation ${scenario.operation}`);
}
