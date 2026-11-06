// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StepList, StepRef, forceFail, goTo, gracefulComplete, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetPageStep, type Connection, type GetPageInput, type Page } from '@superdurable/connector-atlassian-confluence';

/** Static Dex Web connection name used by the Worker and starter. */
export const ConnectionName = 'confluence-policy';
/** Safe summary returned by this sample Flow. */
export interface PageBrief { pageId: string; title: string; versionNumber: number; }

/** Read one Confluence page using the static connection. */
export class ReadConfluencePageFlow implements Flow<GetPageInput> {
  private readonly prepare = new PrepareReadPage();
  private readonly complete = new CompleteReadPage();
  /** Bind the named connection at Worker registration. */
  constructor(private readonly connection: Connection) { if (!connection) throw new TypeError('Confluence connection is required'); }
  /** Stable Flow identity. */ getFlowType(): string { return 'ConfluenceReadPageFlow'; }
  /** Register preparation, provider read, and summary Steps. */
  getSteps(): StepList<GetPageInput> {
    return StepList.startStep(this.prepare).otherSteps(
      newGetPageStep({
        stepType: 'ReadConfluencePage',
        annotations: { groupId: 'confluence-read', groupLabel: 'Confluence Page', explanation: 'Read one page through the configured Confluence site.' },
        connection: this.connection, connectionName: ConnectionName, mapToOperationInput: input => input,
        found: this.complete,
      }), this.complete,
    );
  }
  /** No Attributes or Streams are persisted. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [] }; }
}

/** Validate the page identity before invoking the provider Step. */
// dex:group group-id:confluence-read group-label:"Confluence Page"
// dex:explanation text:"Check the page ID before reading it from the selected Confluence site."
export class PrepareReadPage implements Step<GetPageInput> {
  /** Stable Step identity. */ getStepType(): string { return 'PrepareConfluenceReadPage'; }
  /** Route valid IDs to the provider Step. */
  execute(_context: Context, input: GetPageInput): StepDecision {
    if (!/^[1-9][0-9]{0,18}$/.test(input.pageId.trim())) return forceFail('Confluence pageId must be numeric');
    return goTo(new StepRef('ReadConfluencePage'), input);
  }
}

/** Complete with safe page metadata. */
// dex:group group-id:confluence-read group-label:"Confluence Page"
// dex:explanation text:"Return the page title and current version after the provider read succeeds."
export class CompleteReadPage implements Step<QueryResult<Page>> {
  /** Stable Step identity. */ getStepType(): string { return 'CompleteConfluenceReadPage'; }
  /** Complete without persisting page body content. */
  execute(_context: Context, result: QueryResult<Page>): StepDecision {
    return gracefulComplete({ pageId: result.value.id, title: result.value.title, versionNumber: result.value.version.number } satisfies PageBrief);
  }
}
