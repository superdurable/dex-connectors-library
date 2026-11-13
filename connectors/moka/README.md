# Moka connector

This connector implements Moka's read-only [job function list](https://www.mokahr.com/docs/api/index.html#-26). It requests `GET https://api.mokahr.com/api-platform/v1/jobs-groupedby-zhineng/{orgId}` with the selected `social` or `campus` recruitment mode. Its output contains only function ID, label, parent ID, and job count. It excludes candidate details, recruiter details, and provider error bodies.

[Moka's API guide](https://www.mokahr.com/docs/api/index.html#basic-auth) says the private API Key is issued by the organization's Customer Success Manager, is used as the HTTP Basic Auth username with an empty password, and all requests must use HTTPS. Ask the CSM for the key and enter it as a Dex secret. In Moka Recruitment Website Management, copy the recruitment website link; the `orgId` is the segment after `/apply/` in `https://app.mokahr.com/apply/{orgId}/{siteId}`. Leave mode blank to use social hiring, or select campus hiring. Set the Dex worker's outbound IP in Moka's Open API allowlist if your organization uses it.

The runnable [job-function example](go/examples/list-job-functions/README.md) demonstrates the generated operation-specific Step and static `moka` connection. Real Moka authorization remains to be tested with a user-controlled Dex Web Connection.
