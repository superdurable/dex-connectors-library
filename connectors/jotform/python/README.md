# Jotform Python connector

This package implements `listForms` and `getSubmission`. Both Queries replay Go-recorded provider branches, retries, requests, and outputs. `getSubmission` returns only ID, form ID, status, and creation timestamp; answers stay outside Flow results.

Create a read-only API key in [Jotform My Account > API](https://www.jotform.com/myaccount/api) and save it in the Dex `api_key` secret field. Select the US, EU, or HIPAA API endpoint for your account. See the [connector README](../README.md) and [runnable Flow](examples/form_inspection/README.md). Live Jotform authorization remains unverified.
