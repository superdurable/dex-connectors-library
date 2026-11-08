# Jotform TypeScript connector

This package implements `listForms` and `getSubmission` for Dex TypeScript Flows. Both Queries replay Go-recorded provider requests, branches, retries, and exact outputs. Submission answers stay outside Flow results.

Create a read-only API key in [Jotform My Account > API](https://www.jotform.com/myaccount/api) and save it in the Dex Web secret `api_key` field. Select the US, EU, or HIPAA endpoint for your account. See the [connector README](../README.md) and the [runnable form inspection Flow](examples/form-inspection/README.md). Live Jotform authorization remains unverified.
