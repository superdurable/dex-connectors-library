// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

export * from './generated/models.js';
export { ListMembersInputSchema } from './schema.js';
export * from './generated/definitions.js';
export * from './generated/steps.js';
export { Connection, newConnection, newConnectionWithOperations } from './generated/connection.js';
export { newProjectConnection } from './project.js';
export { MailchimpClient, type MailchimpClientOptions, type MailchimpRequestSender } from './client.js';
export { GetMemberOperation, ListMembersOperation, UpsertMemberOperation, UpdateMemberTagsOperation, SendCampaignOperation } from './operations.js';
export { MinimumPollIntervalMs } from './client.js';
export { MemberChangedEventSchema, MemberChangedTriggerConfigurationSchema, MemberStatuses, type MemberChangedEvent, type MemberChangedTriggerConfiguration } from './member_snapshot.js';
export {
  MemberChangedTriggerDefinition, UIAudienceIDEntryPortAudienceID, UIMemberStatusPickerPortStatuses, UIUnitAudienceIDEntry, UIUnitMemberStatusPicker,
  decodeMemberChangedEventPayload, defineMemberChangedTriggerBinding, newMemberChangedTrigger, newMemberTriggerRunner, newProjectMemberTriggerRunner,
  type MemberChangedTriggerBindingConfig, type MemberChangedTriggerConfig, type MemberChangedTriggerRoute, type MemberTriggerRunnerOptions, type ProjectMemberChangedTriggerRoute,
} from './trigger.js';
