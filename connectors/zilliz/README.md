# Zilliz Cloud connector

This connector lists clusters accessible to a Zilliz Cloud API Key using the official [List Clusters V2](https://docs.zilliz.com/reference/restful/list-clusters-v2) control-plane API. `ListClusters` reads the first page of at most 25 clusters and returns only ID, name, region, plan, and status. It does not expose cluster endpoints or vector data. The [Zilliz China website](https://zilliz.com.cn/terms-and-conditions) is operated by 上海赜睿信息科技有限公司.

## Connection

Sign in at [Zilliz Cloud](https://cloud.zilliz.com), open organization > API Keys > + API Key, and create a customized Key with project Read-Only permission for the intended clusters. [API Key guidance](https://docs.zilliz.com/docs/manage-api-keys) explains role restrictions. Copy the Key into Dex Web Connections. Leave timeout and maximum response size blank for the displayed defaults.

Run the [example Flow](go/examples/list-clusters/) with `{}` input. The checked-in mock returns one cluster. Live Zilliz authorization and cluster behavior require a user-provided Key and have not been verified here.
