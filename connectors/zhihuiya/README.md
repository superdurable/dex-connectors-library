# Zhihuiya Patent Valuation

This connector reads one patent's valuation metrics from the [Zhihuiya Data API](https://developers.zhihuiya.com/devportal/api-reference/patent-valuation-data/patent_valuation). The [official privacy policy](https://www.zhihuiya.com/legal/privacy) identifies 智慧芽信息科技（苏州）有限公司 as the Chinese service operator.

Sign in at [Zhihuiya](https://developers.zhihuiya.com), open the management console's API 密钥 page, and create a secret `sk-` API Key with access to patent valuation. Copy the key into the `zhihuiya` Dex Web connection. Enter one patent publication number such as `JP6012277B2` in `publicationNumber`. The connector sends an HTTPS Bearer request and returns the matching publication number, patent ID, estimated value in US dollars, and five provider scores. It discards patent abstracts, parties, the raw response, and the key. The provider reports that valuation coverage excludes WO patents and updates quarterly. Blank timeout and maxResponseBytes use their displayed defaults.

The [runnable Dex example](go/examples/get-patent-valuation/) uses a synthetic mock. Live account entitlement, authorization, valuation availability, and exact values remain unverified without user-controlled provider credentials.
