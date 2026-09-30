// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

export interface HubSpotOwner { id: string; displayName: string; email?: string; isQueue: boolean; }
export interface HubSpotOwnerPage { owners: HubSpotOwner[]; nextCursor: string; }
export interface HubSpotDealStage { id: string; label: string; isClosed: boolean; }
export interface HubSpotDealPipeline { id: string; label: string; stages: HubSpotDealStage[]; }

const numericIDPattern = /^[0-9]{1,20}$/;

/** parseHubSpotOwnerPage keeps active owners from one /crm/owners/2026-09 page and its after cursor. */
export function parseHubSpotOwnerPage(value: Record<string, unknown>): HubSpotOwnerPage {
  if (!Array.isArray(value.results)) throw new Error(hubspotErrorText(value, "HubSpot returned no owner list"));
  const owners = value.results.flatMap((item) => {
    if (!record(item) || !text(item.id) || !numericIDPattern.test(item.id) || item.archived === true) return [];
    const name = [item.firstName, item.lastName].filter(text).join(" ");
    const email = text(item.email) ? item.email : undefined;
    return [{id: item.id, displayName: name || email || item.id, email, isQueue: item.type === "QUEUE"}];
  });
  const paging = record(value.paging) && record(value.paging.next) ? value.paging.next : {};
  return {owners, nextCursor: text(paging.after) ? paging.after : ""};
}

/** parseHubSpotDealPipelines keeps active deal pipelines and stages in HubSpot's display order. */
export function parseHubSpotDealPipelines(value: Record<string, unknown>): HubSpotDealPipeline[] {
  if (!Array.isArray(value.results)) throw new Error(hubspotErrorText(value, "HubSpot returned no deal pipelines"));
  return byDisplayOrder(value.results).flatMap((pipeline) => {
    if (!text(pipeline.id) || !text(pipeline.label) || pipeline.archived === true) return [];
    const stages = byDisplayOrder(Array.isArray(pipeline.stages) ? pipeline.stages : []).flatMap((stage) => {
      if (!text(stage.id) || !text(stage.label) || stage.archived === true) return [];
      const metadata = record(stage.metadata) ? stage.metadata : {};
      return [{id: stage.id, label: stage.label, isClosed: metadata.isClosed === "true" || metadata.isClosed === true}];
    });
    return [{id: pipeline.id, label: pipeline.label, stages}];
  });
}

/** isHubSpotOwnerID reports whether value is blank or a numeric HubSpot owner ID. */
export function isHubSpotOwnerID(value: string): boolean {
  return value === "" || numericIDPattern.test(value);
}

// hubspotErrorText names only HubSpot's error category, never its message text.
function hubspotErrorText(value: Record<string, unknown>, fallback: string): string {
  return text(value.category) && /^[A-Z_]{1,64}$/.test(value.category) ? `${fallback} (${value.category})` : fallback;
}

function byDisplayOrder(values: unknown[]): Record<string, unknown>[] {
  return values.filter(record).sort((left, right) => displayOrder(left) - displayOrder(right));
}

function displayOrder(value: Record<string, unknown>): number {
  return typeof value.displayOrder === "number" ? value.displayOrder : Number.MAX_SAFE_INTEGER;
}

function record(value: unknown): value is Record<string, unknown> { return typeof value === "object" && value !== null && !Array.isArray(value); }
function text(value: unknown): value is string { return typeof value === "string" && value.trim().length > 0; }
