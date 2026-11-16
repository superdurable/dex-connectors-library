// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, StaticCredentialProvider, newTriggerContext } from '@superdurable/connectors-sdk';
import { SalesforceClient } from '../../src/client.js';
import { RecordPollFetcher } from '../../src/record_modified_fetcher.js';
import { validateRecordModifiedTriggerConfiguration, type RecordModifiedTriggerConfiguration } from '../../src/record_modified.js';
import type { Credentials } from '../../src/models.js';
import { FakeInstanceURL, SalesforceOrgFake } from './salesforce_org_fake.js';

/** The fake's only accepted session; an inert fixture value. */
export const pollTestToken = 'salesforce-poll-' + 'token';
/** A Trigger context that is never cancelled. */
export const context = newTriggerContext();

/** A manual clock shared by the client and the fake org. */
export class ManualClock {
  private milliseconds = Date.UTC(2026, 2, 2, 9, 0, 0);
  /** Read the clock. */ readonly now = (): Date => new Date(this.milliseconds);
  /** Move the clock. */ advance(milliseconds: number): void { this.milliseconds += milliseconds; }
}

/** A fetcher over a fake org, both on one manual clock starting 2026-03-02T09:00:00Z. */
export function newPollTestFetcher(pageSize: number, configuration: RecordModifiedTriggerConfiguration): [RecordPollFetcher, SalesforceOrgFake, ManualClock] {
  const clock = new ManualClock();
  const org = new SalesforceOrgFake(pollTestToken, clock.now);
  const reference = { provider: 'salesforce', name: 'salesforce-crm' };
  const credentials = new StaticCredentialProvider<Credentials>([[reference, { authMethodId: 'salesforce-oauth', oauthClientId: 'inert-consumer-key', accessToken: new SecretString(pollTestToken), instanceUrl: FakeInstanceURL }]]);
  const client = new SalesforceClient({ pollPageSize: pageSize }, credentials, { send: org.send, now: clock.now });
  validateRecordModifiedTriggerConfiguration(configuration);
  return [new RecordPollFetcher(client, reference, configuration), org, clock];
}

