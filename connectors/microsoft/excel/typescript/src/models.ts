// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

export * from './generated/models.js';
export { ConnectorID, ProviderID } from './generated/definitions.js';

/** Delegated Microsoft work or school OAuth method. */
export const OAuthAuthMethodID = 'microsoft-oauth';

/** A literal text cell, including formula-like text. */
export function textCellValue(value: string): string { return value; }
/** A finite Excel number. */
export function numberCellValue(value: number): number {
  if (!Number.isFinite(value)) throw new TypeError('Excel number must be finite');
  return value;
}
/** A Boolean Excel cell. */
export function booleanCellValue(value: boolean): boolean { return value; }
/** An empty Excel cell. */
export function emptyCellValue(): string { return ''; }
