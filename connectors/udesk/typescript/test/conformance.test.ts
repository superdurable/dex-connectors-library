// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { UdeskClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, SyntheticRandomSource, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/udesk/scenarios.json', import.meta.url));
const connection = { provider: 'udesk', name: 'udesk' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeUdesk, ''));

async function invokeUdesk(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'listTicketStatuses') throw new Error(`unknown Udesk operation ${scenario.operation}`);
  const isDefect = scenario.name.endsWith('-defect');
  const credentials = new StaticCredentialProvider([[connection, { adminEmail: new SecretString(isDefect ? '' : 'REDACTED_ADMIN@example.invalid'), apiToken: new SecretString('REDACTED_API_TOKEN') }]]);
  const client = new UdeskClient({ baseUrl: 'https://demo.udesk.cn' }, credentials, { send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)), now: () => new Date(scenario.fixedClock ?? '2026-11-19T12:00:00Z'), nonce: () => new SyntheticRandomSource(scenario.randomSeed ?? 'SYNTHETIC_CONFORMANCE_UDESK').read(16) });
  const call: Call = {
    context: { flowId: 'udesk-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'udesk', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.listTicketStatuses().invoke(call, scenario.input as unknown as Parameters<ReturnType<UdeskClient['listTicketStatuses']>['invoke']>[1]);
}
