# GitCode repository lookup

The Go connector performs the official read-only [repository lookup](https://docs.gitcode.com/docs/apis/get-api-v-5-repos-owner-repo/). It returns only repository ID, full name, visibility, and default branch; creator email, membership, and other private response fields are dropped.

GitCode [API authentication](https://docs.gitcode.com/docs/apis/) accepts a personal access token in an HTTPS `Authorization: Bearer` header. Create a token in [personal settings](https://docs.gitcode.com/docs/help/home/user_center/security_management/user_pat/). The default rate limit is 400 requests per minute and 4000 per hour.

`getRepository` selects `fetched`, `notFound`, `providerRejected`, `invalidResponse`, or `defect`. Rate limits, transport errors, and temporary server faults retry. Optional branches fail the example Flow unless wired by an application.

[Run the repository Flow](go/examples/repository/README.md). Its checked-in mock is synthetic; live repository lookup has not been verified without a user-controlled token in Dex Web Connections.
