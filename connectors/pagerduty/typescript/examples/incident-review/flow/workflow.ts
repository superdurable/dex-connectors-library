// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetIncidentStep, newListIncidentsStep, type Connection, type GetIncidentInput, type Incident, type IncidentPage, type ListIncidentsInput } from '@superdurable/connector-pagerduty';

/** Dex Web connection used by this example. */
export const ConnectionName = 'pagerduty';
/** Safe page persisted by the Flow. */
export const incidentPage = new Attribute<IncidentPage>('pagerduty-incident-page', jsonCodec<IncidentPage>());
/** Safe incident detail persisted by the lookup Flow. */
export const incidentDetail = new Attribute<Incident>('pagerduty-incident-detail', jsonCodec<Incident>());

/** Review one bounded incident page. */
export class IncidentReviewFlow implements Flow<ListIncidentsInput> {
  private readonly prepare = new PrepareIncidentReview();
  private readonly complete = new CompleteIncidentReview();
  /** Bind the named provider connection. */ constructor(private readonly connection: Connection) {
    if (!connection) throw new TypeError('PagerDuty connection is required');
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'PagerDutyIncidentReviewFlow'; }
  /** Register validation, provider query, and completion. */
  getSteps(): StepList<ListIncidentsInput> {
    return StepList.startStep(this.prepare).otherSteps(
      newListIncidentsStep({
        stepType: 'ListPagerDutyIncidents',
        annotations: { groupId: 'pagerduty-review', groupLabel: 'PagerDuty Incidents', explanation: 'List one bounded incident page.' },
        connection: this.connection, connectionName: ConnectionName,
        mapToOperationInput: input => input,
        listed: this.complete,
      }),
      this.complete,
    );
  }
  /** Register persisted page. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [incidentPage] }; }
}

/** Validate the requested window. */
// dex:group group-id:pagerduty-review group-label:"PagerDuty Incidents"
// dex:explanation text:"Validate the incident page window before querying PagerDuty."
export class PrepareIncidentReview implements Step<ListIncidentsInput> {
  /** Stable Step identity. */ getStepType(): string { return 'PreparePagerDutyIncidentReview'; }
  /** Start the provider query. */ execute(_context: Context, input: ListIncidentsInput): StepDecision {
    if (!Number.isInteger(input.limit) || input.limit < 1 || input.limit > 100 || !Number.isInteger(input.offset) || input.offset < 0) return forceFail('Invalid incident window');
    return goTo(new StepRef('ListPagerDutyIncidents'), input);
  }
}

/** Retain the page and complete. */
// dex:group group-id:pagerduty-review group-label:"PagerDuty Incidents"
// dex:explanation text:"Persist the listed incident page and finish the review."
export class CompleteIncidentReview implements Step<QueryResult<IncidentPage>> {
  /** Stable Step identity. */ getStepType(): string { return 'CompletePagerDutyIncidentReview'; }
  /** Persist the safe provider result. */ execute(context: Context, result: QueryResult<IncidentPage>): StepDecision {
    incidentPage.set(context, result.value);
    return gracefulComplete(result.value);
  }
}

/** Look up one PagerDuty incident by provider ID. */
export class IncidentLookupFlow implements Flow<GetIncidentInput> {
  private readonly complete = new CompleteIncidentLookup();
  /** Bind the named provider connection. */ constructor(private readonly connection: Connection) {
    if (!connection) throw new TypeError('PagerDuty connection is required');
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'PagerDutyIncidentLookupFlow'; }
  /** Register the provider query and completion. */
  getSteps(): StepList<GetIncidentInput> {
    return StepList.startStep(newGetIncidentStep<GetIncidentInput>({
      stepType: 'GetPagerDutyIncident',
      annotations: { groupId: 'pagerduty-lookup', groupLabel: 'PagerDuty Incident', explanation: 'Get the selected incident.' },
      connection: this.connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: this.complete,
    })).otherSteps(this.complete);
  }
  /** Register the retained detail. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [incidentDetail] }; }
}

/** Retain the incident detail and complete. */
// dex:group group-id:pagerduty-lookup group-label:"PagerDuty Incident"
// dex:explanation text:"Persist the selected incident detail and finish the lookup."
export class CompleteIncidentLookup implements Step<QueryResult<Incident>> {
  /** Stable Step identity. */ getStepType(): string { return 'CompletePagerDutyIncidentLookup'; }
  /** Persist the safe provider result. */ execute(context: Context, result: QueryResult<Incident>): StepDecision {
    incidentDetail.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
