#!/usr/bin/env bash
# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT

set -euo pipefail

if [[ "${1:-}" != "" && "${1:-}" != "--apply" ]]; then
  echo 'usage: script/npm-configure-trusted-publishers.sh [--apply]' >&2
  exit 2
fi

while IFS= read -r package_file; do
  package_name="$(node -p 'require(process.argv[1]).name' "$(pwd)/${package_file}")"
  workflow=release-connector.yml
  if [[ "${package_file}" == sdkts/package.json ]]; then workflow=release-sdk-ts.yml; fi
  if ! npm view "${package_name}" version >/dev/null 2>&1; then
    echo "Skip unpublished package ${package_name}"
    continue
  fi
  trust_list="$(npm trust list "${package_name}" --json 2>/dev/null)" || {
    echo "Cannot inspect trusted publishers for ${package_name}; sign in with npm 2FA" >&2
    exit 1
  }
  if grep -Fq "${workflow}" <<<"${trust_list}"; then
    echo "Trusted publisher already configured for ${package_name}"
    continue
  fi
  if [[ "${1:-}" == --apply ]]; then
    npm trust github "${package_name}" \
      --repository superdurable/dex-connectors-library \
      --file "${workflow}" --allow-publish
  else
    printf 'Would configure trusted publisher for %s using %s\n' "${package_name}" "${workflow}"
  fi
done < <(
  {
    if [[ -f sdkts/package.json ]]; then printf '%s\n' sdkts/package.json; fi
    while IFS= read -r connector_directory; do
      package_file="${connector_directory}/typescript/package.json"
      if [[ -f "${package_file}" ]]; then printf '%s\n' "${package_file}"; fi
    done < <(awk '/^  - connectors\// {print $2}' catalog.yaml)
  } | sort
)
