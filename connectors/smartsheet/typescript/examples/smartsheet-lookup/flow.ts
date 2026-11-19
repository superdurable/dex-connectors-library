// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StepList, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetSheetStep, newListSheetsStep, type Connection, type GetSheetInput, type Sheet, type SheetPage } from '../../src/index.js';

/** Static Dex Web connection name. */ export const ConnectionName = 'smartsheet';
/** Start request for one sheet page. */ export interface Request { page?: number }
/** Completed sheet lookup. */ export interface Outcome { sheet: Sheet | null }

/** Read a sheet page and then its first sheet through two provider Queries. */
export class SmartsheetLookupFlow implements Flow<Request> {
  private readonly finish = new FinishLookup();
  private readonly getSheet;
  private readonly afterList;
  private readonly listSheets;
  private readonly start;

  /** Bind a static Smartsheet connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Smartsheet lookup requires its static connection');
    this.getSheet = newGetSheetStep<GetSheetInput>({ stepType: 'ReadSmartsheetSheet', annotations: { groupId: 'smartsheet-lookup', groupLabel: 'Smartsheet lookup', explanation: 'Read the first sheet returned by the list.' }, connection, connectionName: ConnectionName, mapToOperationInput: input => input, found: this.finish });
    this.afterList = new AfterList(this.getSheet);
    this.listSheets = newListSheetsStep<Request>({ stepType: 'ListSmartsheetSheets', annotations: { groupId: 'smartsheet-lookup', groupLabel: 'Smartsheet lookup', explanation: 'List one page of accessible sheets.' }, connection, connectionName: ConnectionName, mapToOperationInput: input => ({ page: input.page ?? 1, pageSize: 10 }), listed: this.afterList });
    this.start = new StartLookup(this.listSheets);
  }
  /** Return the stable Flow identity. */ getFlowType(): string { return 'SmartsheetTypeScriptLookup'; }
  /** Register all application and connector Steps. */ getSteps(): StepList<Request> { return StepList.startStep<Request>(this.start).otherSteps(this.listSheets, this.afterList, this.getSheet, this.finish); }
  /** This Flow keeps no Attributes or Streams. */ getPersistenceSchema(): PersistenceSchema { return {}; }
}

// dex:group group-id:smartsheet-lookup group-label:"Smartsheet lookup"
// dex:explanation text:"Start the Smartsheet read with a typed request."
class StartLookup implements Step<Request> {
  readonly inputCodec = jsonCodec<Request>();
  constructor(private readonly next: Step<Request>) {}
  getStepType(): string { return 'StartSmartsheetTypeScriptLookup'; }
  execute(_context: Context, input: Request): StepDecision { return goTo(this.next, input); }
}

// dex:group group-id:smartsheet-lookup group-label:"Smartsheet lookup"
// dex:explanation text:"Choose the first accessible sheet for the second read."
class AfterList implements Step<QueryResult<SheetPage>> {
  constructor(private readonly next: Step<GetSheetInput>) {}
  getStepType(): string { return 'AfterSmartsheetList'; }
  execute(_context: Context, result: QueryResult<SheetPage>): StepDecision {
    const first = result.value.sheets?.[0];
    return first ? goTo(this.next, { sheetId: first.id }) : gracefulComplete({ sheet: null } satisfies Outcome);
  }
}

// dex:group group-id:smartsheet-lookup group-label:"Smartsheet lookup"
// dex:explanation text:"Complete the Flow with the requested sheet."
class FinishLookup implements Step<QueryResult<Sheet>> {
  getStepType(): string { return 'FinishSmartsheetLookup'; }
  execute(_context: Context, result: QueryResult<Sheet>): StepDecision { return result.value.id > 0 ? gracefulComplete({ sheet: result.value } satisfies Outcome) : forceFail('Smartsheet returned no sheet'); }
}
