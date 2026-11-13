# Get Note task status Flow

Run `go run .` here with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev` and Dex project configuration available to `projectconfig.LoadFromEnvironment`. Add a `dedao` connection in Dex Web using the API Key and Client ID from [Get Note Open Platform](https://www.biji.com/openapi). Start `DedaoNoteTaskStatus` with `{"taskID":"69c3995e99f5a67e"}`, using a task ID returned by a prior asynchronous note-save call. The default `completedTask` mock returns a completed note ID; `pendingTask` returns pending. A real connection reads status only, not note content.
