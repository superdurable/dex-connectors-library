# Hootsuite for Dex Python

This package implements `listSocialProfiles`, `listScheduledPosts`, and `schedulePost` through the released Dex Python connector SDK. It uses the generated Hootsuite connection, typed operation models, Step factories, and OAuth refresh declaration.

See the [connector guide](../README.md) for Hootsuite Developer Portal setup. `schedulePost` can create a real scheduled message. If it returns `uncertain`, inspect Hootsuite Planner before requesting another post.

The Go-recorded provider scenarios in `conformance/connectors/hootsuite/scenarios.json` are replayed by this package's tests. Live Hootsuite authorization remains unverified.
