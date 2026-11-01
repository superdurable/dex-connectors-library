# Mixpanel connector for Python

Install with `pip install dex-connector-mixpanel==0.1.0`. The package requires
`dex-connectors-sdk==0.1.0` and `dex-python-sdk>=1.28.0`, and implements the
manifest operations `trackEvent` (Mutation) and `exportEvents` (Query) with the
same branches, defaults, and failure classification as the Go module.

## Operations

| Operation | Factory | Happy branch | Optional branches |
| --- | --- | --- | --- |
| `trackEvent` | `new_track_event_step` | `imported` | `providerRejected`, `invalidResponse`, `defect` |
| `exportEvents` | `new_export_events_step` | `exported` | `providerRejected`, `invalidResponse`, `defect` |

`trackEvent` posts one event to `/import?strict=1` with the caller's stable
`time` and `distinct_id` and a UUID-shaped `$insert_id` derived from the SHA-256
of the stable call ID, so Mixpanel deduplicates retries. Property names `time`,
`distinct_id`, `$insert_id`, `ip`, and blank names select `defect`. Only a
strict confirmation of one imported record selects `imported`.

`exportEvents` reads raw JSONL for an inclusive `YYYY-MM-DD` range of at most
30 days with a limit from 1 through 100, and returns only each event's name,
distinct ID, time, and insert ID. Raw exports may contain duplicates.

Both operations retry on 408, 409, 429, 5xx, and transport failures; 401 and
403 select `providerRejected` with authentication and authorization kinds.

## Authorization

Create a Dex Web connection named `mixpanel` as described in the
[connector README](../README.md#authorization). The service-account username
and secret travel only in the HTTP Basic `Authorization` header and are
resolved for every attempt.

## Example

[`examples/event_track`](examples/event_track/flow.py) imports one event and
fixes a missing timestamp to the Flow start time. Run the Worker with
`uv run python -m examples.event_track.worker` after `dexcli dev` has loaded
a project containing the `mixpanel` connection.

## Tests

`mixpanelmock.Mock` scripts both operations and defaults to the manifest
answers. Run `uv run pytest tests` for provider parity and manifest contract
checks.
