# Hootsuite for Dex TypeScript

This package implements `listSocialProfiles`, `listScheduledPosts`, and `schedulePost` with generated typed Steps, a renewable OAuth connection, and bounded provider requests. See the [connector guide](../README.md) for Hootsuite app setup.

`schedulePost` can create a real scheduled message. If the outcome is `uncertain`, inspect Hootsuite Planner before asking for another post. Go-recorded provider scenarios are replayed in `test/conformance.test.ts`; live Hootsuite authorization remains unverified.

The runnable [social profiles Flow](examples/social-profiles/README.md) uses the static `hootsuite` Dex Web connection. Run `npm ci` and `npm run check` from this package. The Flow's real Dex integration test uses a local Hootsuite fixture and does not need provider credentials.
