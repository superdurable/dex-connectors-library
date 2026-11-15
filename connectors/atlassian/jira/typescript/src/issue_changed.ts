// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mustTriggerBindingDefinition, type ConnectorConfigurationUI, type TriggerBindingDefinition, type TriggerDefinition } from '@superdurable/connectors-sdk';
import { z } from 'zod';
import { ConnectorID } from './generated/definitions.js';
import { IssueSchema } from './generated/models.js';

/** A new issue: Jira's updated time is within one second of its created time. */
export const IssueChangeCreated = 'created';
/**
 * Any other change that moved the issue's updated time, such as an edited field, a transition, an assignment,
 * or a comment, including an edit made soon after creation but before the next poll read the issue.
 */
export const IssueChangeUpdated = 'updated';
/** The derived kind of one issueChanged delivery. */
export type IssueChangeKind = typeof IssueChangeCreated | typeof IssueChangeUpdated;

/** Jira project picker UI unit, which stores the project ID, key, and name. */ export const UIUnitProjectPicker = 'projectPicker';
/** Project picker output port: the numeric project ID. */ export const UIProjectPickerPortProjectID = 'projectId';
/** Project picker output port: the project key. */ export const UIProjectPickerPortProjectKey = 'projectKey';
/** Project picker output port: the project name. */ export const UIProjectPickerPortProjectName = 'projectName';

/**
 * The issueChanged binding configuration that the project picker saves in Dex Web; field names match the Go
 * connector. Blank projectId and projectKey watch every project the connection's account can browse.
 * Changing the project of a binding that already polled makes its next poll record a new baseline.
 */
export const IssueChangedTriggerConfigurationSchema = z.strictObject({
  /** Numeric Jira project ID from the project picker, such as 10000; preferred to projectKey because it survives a key change. */ projectId: z.string().optional(),
  /** Project key, such as OPS; selects the project when projectId is blank. */ projectKey: z.string().optional(),
  /** The picked project's display name, kept for display only. */ projectName: z.string().optional(),
}).superRefine((configuration, context) => {
  if (configuration.projectId && !/^[1-9][0-9]{0,17}$/.test(configuration.projectId)) context.addIssue({ code: 'custom', message: 'issueChanged projectId must be a numeric Jira project ID such as 10000' });
  if (configuration.projectKey && !/^[A-Z][A-Z0-9_]{0,254}$/.test(configuration.projectKey)) context.addIssue({ code: 'custom', message: 'issueChanged projectKey must be an uppercase Jira project key such as OPS' });
});
/** The watched project of one issueChanged binding. */
export type IssueChangedTriggerConfiguration = z.infer<typeof IssueChangedTriggerConfigurationSchema>;

/**
 * One issue created or updated in the watched scope, as Jira's enhanced search returned it; field names match
 * the Go connector, so a Go and a TypeScript replica share an inbox. Several edits made between two polls
 * arrive as one change carrying the latest state.
 */
export const IssueChangedEventSchema = z.strictObject({
  /** The Jira site the issue belongs to. */ cloudId: z.string(),
  /** The derived change kind. */ changeKind: z.enum([IssueChangeCreated, IssueChangeUpdated]),
  /** The issue's standard fields from the search page; description is never set. */ issue: IssueSchema,
});
/** One issue created or updated in the watched scope. */
export type IssueChangedEvent = z.infer<typeof IssueChangedEventSchema>;

/** Manifest definition of issueChanged. */
export const IssueChangedTriggerDefinition: TriggerDefinition = {
  trigger: { connectorId: ConnectorID, triggerName: 'issueChanged' },
  description: 'Receive each Jira issue created or updated in one project, or in every project the account can browse, read by polling enhanced JQL search on the updated time; the first run records a baseline and delivers no existing issues.',
};

/** One static application binding of issueChanged. */
export interface IssueChangedTriggerBindingConfig {
  /** Static connection name. */ connectionName: string;
  /** Static binding name. */ bindingName: string;
  /** Connector Studio configuration units for the binding, such as the projectPicker unit. */ configurationUI: ConnectorConfigurationUI;
}

/** Declare a static issueChanged binding for Flow Definition tooling and Dex Web. */
export function defineIssueChangedTriggerBinding(config: IssueChangedTriggerBindingConfig): TriggerBindingDefinition {
  return mustTriggerBindingDefinition({ definition: IssueChangedTriggerDefinition, ...config });
}
