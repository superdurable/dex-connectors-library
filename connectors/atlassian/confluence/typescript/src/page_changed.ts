// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mustTriggerBindingDefinition, type ConnectorConfigurationUI, type TriggerBindingDefinition, type TriggerDefinition } from '@superdurable/connectors-sdk';
import { z } from 'zod';
import { ConnectorID } from './generated/definitions.js';
import { PageSummarySchema } from './generated/models.js';

/** A page whose delivered version is 1, its first published version. */
export const PageChangeCreated = 'created';
/** A page whose delivered version is 2 or later, including a page created and edited again before the next poll read it. */
export const PageChangeUpdated = 'updated';
/** The derived kind of one pageChanged delivery. */
export type PageChangeKind = typeof PageChangeCreated | typeof PageChangeUpdated;

/** Confluence space picker UI unit, which stores the space ID, key, and name. */ export const UIUnitSpacePicker = 'spacePicker';
/** Space picker output port: the numeric space ID. */ export const UISpacePickerPortSpaceID = 'spaceId';
/** Space picker output port: the space key. */ export const UISpacePickerPortSpaceKey = 'spaceKey';
/** Space picker output port: the space display name. */ export const UISpacePickerPortSpaceName = 'spaceName';

/** A Confluence space key, such as OPS or a personal space key such as ~5b10ac8d82e05b22cc7d4ef5. */
export const spaceKeyPattern = /^~?[A-Za-z0-9_-]{1,255}$/;

/**
 * The pageChanged binding configuration that the space picker saves in Dex Web; field names match the Go
 * connector. A blank spaceKey watches every space the connection can view. Changing the space of a binding
 * that already polled makes its next poll record a new baseline.
 */
export const PageChangedTriggerConfigurationSchema = z.strictObject({
  /** The picked space's numeric ID, kept for display only. */ spaceId: z.string().optional(),
  /** Limits the Trigger to pages in this space, such as OPS; blank watches every space the connection can view. */ spaceKey: z.string().optional(),
  /** The picked space's display name, kept for display only. */ spaceName: z.string().optional(),
}).superRefine((configuration, context) => {
  if (configuration.spaceKey && !spaceKeyPattern.test(configuration.spaceKey)) context.addIssue({ code: 'custom', message: 'pageChanged spaceKey must be a Confluence space key such as OPS, without surrounding whitespace' });
});
/** The watched space of one pageChanged binding. */
export type PageChangedTriggerConfiguration = z.infer<typeof PageChangedTriggerConfigurationSchema>;

/**
 * One page created or updated since the previous poll; field names match the Go connector, so a Go and a
 * TypeScript replica share an inbox. A page edited several times between two polls arrives once, at the
 * version current when the poll read it.
 */
export const PageChangedEventSchema = z.strictObject({
  /** The Confluence site the page belongs to. */ cloudId: z.string(),
  /** The derived change kind. */ changeKind: z.enum([PageChangeCreated, PageChangeUpdated]),
  /** The page and its delivered version; read its body with getPage. */ page: PageSummarySchema,
});
/** One page created or updated since the previous poll. */
export type PageChangedEvent = z.infer<typeof PageChangedEventSchema>;

/** Manifest definition of pageChanged. */
export const PageChangedTriggerDefinition: TriggerDefinition = {
  trigger: { connectorId: ConnectorID, triggerName: 'pageChanged' },
  description: 'Receive each page created or updated in one space, or in every space the connection can view, read through Confluence CQL search ordered by last modification; the first run records a baseline and delivers no existing pages.',
};

/** One static application binding of pageChanged. */
export interface PageChangedTriggerBindingConfig {
  /** Static connection name. */ connectionName: string;
  /** Static binding name. */ bindingName: string;
  /** Connector Studio configuration units for the binding, such as the spacePicker unit. */ configurationUI: ConnectorConfigurationUI;
}

/** Declare a static pageChanged binding for Flow Definition tooling and Dex Web. */
export function definePageChangedTriggerBinding(config: PageChangedTriggerBindingConfig): TriggerBindingDefinition {
  return mustTriggerBindingDefinition({ definition: PageChangedTriggerDefinition, ...config });
}
