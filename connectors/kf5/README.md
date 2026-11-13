# KF5 Ticket Read

This connector reads limited status metadata for one support ticket through the [KF5 Tickets API](https://developer.kf5.com/restapi/tickets/). The [official privacy policy](https://www.kf5.com/privacy.html) identifies 成都逸创信息技术有限公司 as the service operator.

Register at [KF5](https://www.kf5.com) and note the subdomain of your tenant URL `https://{subdomain}.kf5.com`. In the tenant, open 个人设置 for the agent email and 系统设置 > 支持渠道 > 开放API设置 for the secret API token. Add a `kf5` Dex Web connection with those values. The connector uses HTTPS Basic authentication with `email/token` and the token, and rejects subdomains containing dots or URL syntax. Agent permission is needed to read a ticket. Pass a positive numeric `ticketID`; the output includes only ID, status, priority, type, and timestamps. Ticket title, description, comments, requester, and raw provider response are discarded. Blank timeout and maxResponseBytes use their displayed defaults.

The [runnable Dex example](go/examples/get-ticket/) uses a synthetic mock and does not verify live tenant authorization or ticket data.
