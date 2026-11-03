// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StepList, StepRef, forceFail, goTo, gracefulComplete, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetLeadStep, newListLeadsStep, type Connection, type GetLeadInput, type Lead, type LeadPage } from '@superdurable/connector-close';

/** Static Dex Web connection used by the Worker and starter. */
export const ConnectionName = 'close';

/** Nonsecret request for a lead lookup. */
export interface CloseLookupRequest { skip?: number }
/** Safe Flow output from the provider. */
export interface CloseLookupOutcome { lead: Lead }

/** List a lead page and fetch its first lead. */
export class CloseLookupFlow implements Flow<CloseLookupRequest> {
  private readonly start = new StartCloseLookup();
  private readonly afterList = new AfterCloseList();
  private readonly complete = new CompleteCloseLookup();

  /** Bind the static connection at Worker registration. */
  constructor(private readonly connection: Connection) {
    if (!connection || connection.reference.name !== ConnectionName) throw new TypeError('Close lookup requires the static close connection');
  }

  /** Stable Dex Flow type. */ getFlowType(): string { return 'CloseTypeScriptLookup'; }
  /** Register start, provider, and completion Steps. */
  getSteps(): StepList<CloseLookupRequest> {
    return StepList.startStep(this.start).otherSteps(
      newListLeadsStep({
        stepType: 'ListCloseLeads',
        annotations: { groupId: 'close-lookup', groupLabel: 'Close lookup', explanation: 'List one bounded Close lead page.' },
        connection: this.connection, connectionName: ConnectionName,
        mapToOperationInput: input => ({ skip: input.skip ?? 0, limit: 10 }),
        listed: this.afterList,
      }),
      this.afterList,
      newGetLeadStep({
        stepType: 'GetCloseLead',
        annotations: { groupId: 'close-lookup', groupLabel: 'Close lookup', explanation: 'Fetch the first listed lead.' },
        connection: this.connection, connectionName: ConnectionName,
        mapToOperationInput: input => input,
        found: this.complete,
      }),
      this.complete,
    );
  }
  /** No durable Attributes or Streams are needed. */ getPersistenceSchema(): PersistenceSchema { return {}; }
}

/** Start the read with validated nonsecret input. */
// dex:group group-id:close-lookup group-label:"Close lookup"
// dex:explanation text:"Start the lookup at the selected zero-based lead offset."
export class StartCloseLookup implements Step<CloseLookupRequest> {
  /** Stable Step type. */ getStepType(): string { return 'StartCloseTypeScriptLookup'; }
  /** Route to the first provider read. */
  execute(_context: Context, input: CloseLookupRequest): StepDecision {
    if (!Number.isSafeInteger(input.skip ?? 0) || (input.skip ?? 0) < 0) return forceFail('skip must be a nonnegative integer');
    return goTo(new StepRef('ListCloseLeads'), input);
  }
}

/** Select the first returned lead for a second provider read. */
// dex:group group-id:close-lookup group-label:"Close lookup"
// dex:explanation text:"Finish an empty page or read its first lead."
export class AfterCloseList implements Step<QueryResult<LeadPage>> {
  /** Stable Step type. */ getStepType(): string { return 'AfterCloseTypeScriptList'; }
  /** Route to getLead only for a nonempty page. */
  execute(_context: Context, result: QueryResult<LeadPage>): StepDecision {
    const first = result.value.leads?.[0];
    if (!first) return gracefulComplete({ lead: { id: '', name: '' } });
    const next: GetLeadInput = { leadId: first.id };
    return goTo(new StepRef('GetCloseLead'), next);
  }
}

/** Complete with the fetched Close lead. */
// dex:group group-id:close-lookup group-label:"Close lookup"
// dex:explanation text:"Complete the lookup with the provider's lead data."
export class CompleteCloseLookup implements Step<QueryResult<Lead>> {
  /** Stable Step type. */ getStepType(): string { return 'CompleteCloseTypeScriptLookup'; }
  /** Return the safe provider result. */
  execute(_context: Context, result: QueryResult<Lead>): StepDecision { return gracefulComplete({ lead: result.value }); }
}
