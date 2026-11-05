// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newFindCustomerByEmailStep, type Connection, type CustomerMatches, type FindCustomerByEmailInput } from '@superdurable/connector-atlassian-jira-service-management';

/** Static connection configured in Dex Web. */
export const ConnectionName = 'jira-service-management-support';
/** Persisted customer lookup output. */
export const customerMatches = new Attribute<CustomerMatches>('jira-service-management-customer-matches', jsonCodec<CustomerMatches>());

/** Find exact email matches in one Jira Service Management desk. */
export class FindCustomerFlow implements Flow<FindCustomerByEmailInput> {
  private readonly prepare = new PrepareCustomerLookup();
  private readonly complete = new CompleteCustomerLookup();
  /** Bind the static Atlassian connection. */ constructor(private readonly connection: Connection) {}
  /** Stable Flow identity. */ getFlowType(): string { return 'JiraServiceManagementFindCustomerFlow'; }
  /** Register the query and completion Steps. */
  getSteps(): StepList<FindCustomerByEmailInput> {
    return StepList.startStep(this.prepare).otherSteps(
      newFindCustomerByEmailStep({
        stepType: 'FindJiraServiceManagementCustomer',
        annotations: { groupId: 'jira-service-management', groupLabel: 'Jira Service Management', explanation: 'Find customers with one exact email address.' },
        connection: this.connection, connectionName: ConnectionName,
        mapToOperationInput: input => input,
        found: this.complete,
      }),
      this.complete,
    );
  }
  /** Register persisted provider output. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [customerMatches] }; }
}

// dex:group group-id:jira-service-management group-label:"Jira Service Management"
// dex:explanation text:"Validate the request before querying Jira Service Management."
/** Check required nonsecret input. */
export class PrepareCustomerLookup implements Step<FindCustomerByEmailInput> {
  /** Stable Step identity. */ getStepType(): string { return 'PrepareJiraServiceManagementCustomerLookup'; }
  /** Start the customer query. */
  execute(_context: Context, input: FindCustomerByEmailInput): StepDecision {
    if (!input.serviceDeskId || !input.email) return forceFail('serviceDeskId and email are required');
    return goTo(new StepRef('FindJiraServiceManagementCustomer'), input);
  }
}

// dex:group group-id:jira-service-management group-label:"Jira Service Management"
// dex:explanation text:"Persist the bounded customer list and complete the Flow."
/** Complete with a bounded customer list. */
export class CompleteCustomerLookup implements Step<QueryResult<CustomerMatches>> {
  /** Stable Step identity. */ getStepType(): string { return 'CompleteJiraServiceManagementCustomerLookup'; }
  /** Persist only safe customer metadata. */
  execute(context: Context, result: QueryResult<CustomerMatches>): StepDecision {
    customerMatches.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
