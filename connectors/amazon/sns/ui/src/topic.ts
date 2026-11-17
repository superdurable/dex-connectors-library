// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

const topicARNPattern = /^arn:aws(?:-us-gov|-cn)?:sns:[a-z0-9-]+:[0-9]{12}:[A-Za-z0-9_-]{1,256}(?:\.fifo)?$/;

/** topicARNFromInput accepts one non-secret SNS topic ARN. */
export function topicARNFromInput(input: string): string | undefined {
  const topicARN = input.trim();
  return topicARNPattern.test(topicARN) ? topicARN : undefined;
}
