// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

/**
 * channelSlugPattern mirrors the Go connector's channel slug rule: a letter or digit, then up to 127
 * letters, digits, hyphens, or underscores.
 */
const channelSlugPattern = /^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$/;

/** savedChannel returns the saved slug, or an empty string when none is saved. */
export function savedChannel(value: unknown): string {
  return typeof value === "string" ? value : "";
}

/** channelError explains why a slug cannot be saved, or returns undefined for a valid or blank slug. */
export function channelError(channel: string, required: boolean): string | undefined {
  if (channel === "") return required ? "Enter a channel slug." : undefined;
  if (channel.includes("://") || channel.includes(".reamaze.")) return "Enter only the slug, such as support, not a URL.";
  if (!channelSlugPattern.test(channel)) return "A channel slug is letters, digits, hyphens, and underscores, such as support.";
  return undefined;
}
