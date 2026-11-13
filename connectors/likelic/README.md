# Likelic Room Directory

`ListRooms` reads one page of group identifiers and notification readiness from the [Likelic Agent API](https://agent.likelic.com/doc/v1/). The [official service terms](https://www.likelic.com/terms/) identify 苏州立刻电子商务有限公司 as the Chinese operator. The connector returns only each room's channel, robot instance ID, room ID, and push state plus the page and total count. Group names, company names, member counts, and the raw provider response are discarded.

Sign in at [Likelic Agent](https://agent.likelic.com), obtain your Customer Code from the enterprise administrator, create a secret `sk-` API Key at 外部拓展 > API 设置, then enable 推送通知 and authorize the Key under 系统设置 > 开放 API 权限. Add the code and Key to a Dex Web `likelic` connection. Pass a one-based page number; each request reads 20 rooms. Blank timeout and maxResponseBytes use the displayed defaults.

The [runnable Dex example](go/examples/list-rooms/) uses a synthetic mock. Live tenant authorization, room visibility, and pagination remain unverified without user-controlled provider credentials.
