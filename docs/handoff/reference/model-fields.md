# Go model field dictionary

Generated from named Go structs with JSON or YAML tags in production source (including SDK Go types). Tables preserve exact tags and Go field types. `omitempty` describes serialization, **not** validation or requiredness. Pointer fields distinguish omission from explicit zero. `-` excludes a field. Untagged embedded fields are shown where present. Inline anonymous request structs and dynamic maps remain defined in the linked handler source; consult the [API guide](../api.md) for wire behavior and [data models](../data-models.md) for validation, relationships, and custom serializers. Source comments below are discovery aids and may describe historical intent; the behavioral guides take precedence over stale comments.

## internal/a2a.rpcRequest

Source: [internal/a2a/server.go](../../../internal/a2a/server.go#L39).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `JSONRPC` | `string` | `jsonrpc` | `` |  |
| `ID` | `json.RawMessage` | `id,omitempty` | `` |  |
| `Method` | `string` | `method` | `` |  |
| `Params` | `json.RawMessage` | `params,omitempty` | `` |  |

## internal/a2a.rpcResponse

Source: [internal/a2a/server.go](../../../internal/a2a/server.go#L46).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `JSONRPC` | `string` | `jsonrpc` | `` |  |
| `ID` | `json.RawMessage` | `id` | `` | ID is NOT omitempty: JSON-RPC 2.0 §5.1 requires the response id to be null when it can't be determined (parse error / invalid request). A nil json.RawMessage marshals to `null`, which is exactly what we want. |
| `Result` | `any` | `result,omitempty` | `` |  |
| `Error` | `*rpcError` | `error,omitempty` | `` |  |

## internal/a2a.rpcError

Source: [internal/a2a/server.go](../../../internal/a2a/server.go#L56).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Code` | `int` | `code` | `` |  |
| `Message` | `string` | `message` | `` |  |
| `Data` | `any` | `data,omitempty` | `` |  |

## internal/a2a.Part

Source: [internal/a2a/server.go](../../../internal/a2a/server.go#L67).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Kind` | `string` | `kind` | `` | "text" &#124; "file" &#124; "data" |
| `Text` | `string` | `text,omitempty` | `` |  |
| `File` | `*FilePart` | `file,omitempty` | `` |  |
| `Data` | `any` | `data,omitempty` | `` |  |

## internal/a2a.FilePart

Source: [internal/a2a/server.go](../../../internal/a2a/server.go#L76).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Name` | `string` | `name,omitempty` | `` |  |
| `MimeType` | `string` | `mimeType,omitempty` | `` |  |
| `Bytes` | `string` | `bytes,omitempty` | `` |  |
| `URI` | `string` | `uri,omitempty` | `` |  |

## internal/a2a.Message

Source: [internal/a2a/server.go](../../../internal/a2a/server.go#L84).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Role` | `string` | `role` | `` | "user" &#124; "agent" |
| `Parts` | `[]Part` | `parts` | `` |  |
| `MessageID` | `string` | `messageId` | `` |  |
| `Kind` | `string` | `kind` | `` | "message" |
| `TaskID` | `string` | `taskId,omitempty` | `` |  |
| `ContextID` | `string` | `contextId,omitempty` | `` |  |

## internal/a2a.Artifact

Source: [internal/a2a/server.go](../../../internal/a2a/server.go#L94).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `ArtifactID` | `string` | `artifactId` | `` |  |
| `Name` | `string` | `name,omitempty` | `` |  |
| `Parts` | `[]Part` | `parts` | `` |  |

## internal/a2a.TaskStatus

Source: [internal/a2a/server.go](../../../internal/a2a/server.go#L101).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `State` | `string` | `state` | `` |  |
| `Message` | `*Message` | `message,omitempty` | `` |  |
| `Timestamp` | `string` | `timestamp,omitempty` | `` |  |

## internal/a2a.Task

Source: [internal/a2a/server.go](../../../internal/a2a/server.go#L108).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `ID` | `string` | `id` | `` |  |
| `ContextID` | `string` | `contextId` | `` |  |
| `Status` | `TaskStatus` | `status` | `` |  |
| `Artifacts` | `[]Artifact` | `artifacts,omitempty` | `` |  |
| `History` | `[]Message` | `history,omitempty` | `` |  |
| `Kind` | `string` | `kind` | `` | "task" |

## internal/a2a.statusUpdateEvent

Source: [internal/a2a/server.go](../../../internal/a2a/server.go#L120).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `TaskID` | `string` | `taskId` | `` |  |
| `ContextID` | `string` | `contextId` | `` |  |
| `Kind` | `string` | `kind` | `` | "status-update" |
| `Status` | `TaskStatus` | `status` | `` |  |
| `Final` | `bool` | `final` | `` |  |

## internal/a2a.artifactUpdateEvent

Source: [internal/a2a/server.go](../../../internal/a2a/server.go#L128).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `TaskID` | `string` | `taskId` | `` |  |
| `ContextID` | `string` | `contextId` | `` |  |
| `Kind` | `string` | `kind` | `` | "artifact-update" |
| `Artifact` | `Artifact` | `artifact` | `` |  |
| `Append` | `bool` | `append` | `` |  |
| `LastChunk` | `bool` | `lastChunk` | `` |  |

## internal/a2a.messageSendParams

Source: [internal/a2a/server.go](../../../internal/a2a/server.go#L278).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Message` | `struct { Role string `json:"role"` Parts []Part `json:"parts"` MessageID string `json:"messageId"` ContextID string `json:"contextId"` TaskID string `json:"taskId"` }` | `message` | `` |  |

## internal/a2a.taskIDParams

Source: [internal/a2a/server.go](../../../internal/a2a/server.go#L393).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `ID` | `string` | `id` | `` |  |

## internal/adapter.AnthropicRequest

Source: [internal/adapter/anthropic.go](../../../internal/adapter/anthropic.go#L20).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Model` | `string` | `model` | `` |  |
| `Messages` | `[]AnthropicMessage` | `messages` | `` |  |
| `System` | `any` | `system,omitempty` | `` | System is a string OR an array of content blocks. The Anthropic Messages API accepts both forms, and real clients (the Anthropic SDK, the Claude CLI / Agent SDK) send the array form, so it must be decoded as `any` and flattened to text via extractAnthropicContent rather than typed as string. |
| `Tools` | `[]AnthropicTool` | `tools,omitempty` | `` |  |
| `MaxTokens` | `int` | `max_tokens,omitempty` | `` |  |
| `Stream` | `bool` | `stream,omitempty` | `` |  |
| `ToolChoice` | `map[string]any` | `tool_choice,omitempty` | `` | ToolChoice is the Anthropic {type:"auto"/"any"/"tool"/"none"} object (optionally carrying name + disable_parallel_tool_use). "none" is always honored (R9-5); any/tool forcing and the parallel cap are enforced under the strict-tools knob (round-11). |
| `Thinking` | `*AnthropicThinkingReq` | `thinking,omitempty` | `` | Thinking is the extended-thinking gate; type "enabled" turns on a synthesized thinking content block (A-04). budget_tokens is advisory. |

## internal/adapter.AnthropicThinkingReq

Source: [internal/adapter/anthropic.go](../../../internal/adapter/anthropic.go#L42).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Type` | `string` | `type` | `` | "enabled" |
| `BudgetTokens` | `int` | `budget_tokens,omitempty` | `` |  |

## internal/adapter.AnthropicMessage

Source: [internal/adapter/anthropic.go](../../../internal/adapter/anthropic.go#L48).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Role` | `string` | `role` | `` |  |
| `Content` | `any` | `content` | `` |  |

## internal/adapter.AnthropicTool

Source: [internal/adapter/anthropic.go](../../../internal/adapter/anthropic.go#L54).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Name` | `string` | `name` | `` |  |
| `Description` | `string` | `description,omitempty` | `` |  |
| `InputSchema` | `map[string]any` | `input_schema,omitempty` | `` |  |
| `CacheControl` | `any` | `cache_control,omitempty` | `` | CacheControl, when present, marks the tool definition as a prompt-cache breakpoint (A-04). |

## internal/adapter.AnthropicResponse

Source: [internal/adapter/anthropic.go](../../../internal/adapter/anthropic.go#L66).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `ID` | `string` | `id` | `` |  |
| `Type` | `string` | `type` | `` |  |
| `Role` | `string` | `role` | `` |  |
| `Content` | `[]AnthropicContent` | `content` | `` |  |
| `Model` | `string` | `model` | `` |  |
| `StopReason` | `string` | `stop_reason` | `` |  |
| `StopSequence` | `*string` | `stop_sequence` | `` |  |
| `Usage` | `AnthropicUsage` | `usage` | `` |  |

## internal/adapter.AnthropicContent

Source: [internal/adapter/anthropic.go](../../../internal/adapter/anthropic.go#L78).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Type` | `string` | `type` | `` |  |
| `Text` | `string` | `text,omitempty` | `` |  |
| `Thinking` | `string` | `thinking,omitempty` | `` | Thinking / Signature carry an extended-thinking block (type "thinking"). |
| `Signature` | `string` | `signature,omitempty` | `` |  |
| `ID` | `string` | `id,omitempty` | `` |  |
| `Name` | `string` | `name,omitempty` | `` |  |
| `Input` | `map[string]any` | `input,omitzero` | `` | omitzero (not omitempty): a tool_use block's `input` is a REQUIRED key — the real API always renders "input": {} for no-arg calls, and strict SDK validation rejects a missing key (round-9 R9-6). Non-tool blocks leave the map nil and the key absent. |

## internal/adapter.AnthropicUsage

Source: [internal/adapter/anthropic.go](../../../internal/adapter/anthropic.go#L97).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `InputTokens` | `int` | `input_tokens` | `` |  |
| `CacheCreationInputTokens` | `*int` | `cache_creation_input_tokens,omitempty` | `` |  |
| `CacheReadInputTokens` | `*int` | `cache_read_input_tokens,omitempty` | `` |  |
| `OutputTokens` | `int` | `output_tokens` | `` |  |

## internal/adapter.anthropicErrorEnvelope

Source: [internal/adapter/anthropic.go](../../../internal/adapter/anthropic.go#L735).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Type` | `string` | `type` | `` | always "error" |
| `Error` | `anthropicErrorBody` | `error` | `` |  |

## internal/adapter.anthropicErrorBody

Source: [internal/adapter/anthropic.go](../../../internal/adapter/anthropic.go#L740).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Type` | `string` | `type` | `` |  |
| `Message` | `string` | `message` | `` |  |

## internal/adapter.AnthropicBatch

Source: [internal/adapter/anthropic_batches.go](../../../internal/adapter/anthropic_batches.go#L49).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `ID` | `string` | `id` | `` |  |
| `Type` | `string` | `type` | `` | always "message_batch" |
| `ProcessingStatus` | `string` | `processing_status` | `` |  |
| `RequestCounts` | `AnthropicBatchRequestCounts` | `request_counts` | `` |  |
| `EndedAt` | `*string` | `ended_at` | `` |  |
| `CreatedAt` | `string` | `created_at` | `` |  |
| `ExpiresAt` | `string` | `expires_at` | `` |  |
| `ArchivedAt` | `*string` | `archived_at` | `` |  |
| `CancelInitiatedAt` | `*string` | `cancel_initiated_at` | `` |  |
| `ResultsURL` | `*string` | `results_url` | `` |  |

## internal/adapter.AnthropicBatchRequestCounts

Source: [internal/adapter/anthropic_batches.go](../../../internal/adapter/anthropic_batches.go#L64).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Processing` | `int` | `processing` | `` |  |
| `Succeeded` | `int` | `succeeded` | `` |  |
| `Errored` | `int` | `errored` | `` |  |
| `Canceled` | `int` | `canceled` | `` |  |
| `Expired` | `int` | `expired` | `` |  |

## internal/adapter.createAnthropicBatchRequest

Source: [internal/adapter/anthropic_batches.go](../../../internal/adapter/anthropic_batches.go#L73).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Requests` | `[]anthropicBatchRequestItem` | `requests` | `` |  |

## internal/adapter.anthropicBatchRequestItem

Source: [internal/adapter/anthropic_batches.go](../../../internal/adapter/anthropic_batches.go#L79).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `CustomID` | `string` | `custom_id` | `` |  |
| `Params` | `json.RawMessage` | `params` | `` |  |

## internal/adapter.anthropicBatchResultLine

Source: [internal/adapter/anthropic_batches.go](../../../internal/adapter/anthropic_batches.go#L85).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `CustomID` | `string` | `custom_id` | `` |  |
| `Result` | `anthropicBatchResult` | `result` | `` |  |

## internal/adapter.anthropicBatchResult

Source: [internal/adapter/anthropic_batches.go](../../../internal/adapter/anthropic_batches.go#L93).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Type` | `string` | `type` | `` | succeeded &#124; errored &#124; canceled &#124; expired |
| `Message` | `json.RawMessage` | `message,omitempty` | `` |  |
| `Error` | `json.RawMessage` | `error,omitempty` | `` |  |

## internal/adapter.Batch

Source: [internal/adapter/batches.go](../../../internal/adapter/batches.go#L54).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `ID` | `string` | `id` | `` |  |
| `Object` | `string` | `object` | `` |  |
| `Endpoint` | `string` | `endpoint` | `` |  |
| `Errors` | `any` | `errors` | `` |  |
| `InputFileID` | `string` | `input_file_id` | `` |  |
| `CompletionWindow` | `string` | `completion_window` | `` |  |
| `Status` | `string` | `status` | `` |  |
| `OutputFileID` | `*string` | `output_file_id,omitempty` | `` |  |
| `ErrorFileID` | `*string` | `error_file_id,omitempty` | `` |  |
| `CreatedAt` | `int64` | `created_at` | `` |  |
| `InProgressAt` | `*int64` | `in_progress_at,omitempty` | `` |  |
| `ExpiresAt` | `*int64` | `expires_at,omitempty` | `` |  |
| `CompletedAt` | `*int64` | `completed_at,omitempty` | `` |  |
| `FailedAt` | `*int64` | `failed_at,omitempty` | `` |  |
| `CancellingAt` | `*int64` | `cancelling_at,omitempty` | `` |  |
| `CancelledAt` | `*int64` | `cancelled_at,omitempty` | `` |  |
| `RequestCounts` | `BatchRequestCounts` | `request_counts` | `` |  |
| `Metadata` | `map[string]any` | `metadata` | `` |  |

## internal/adapter.BatchRequestCounts

Source: [internal/adapter/batches.go](../../../internal/adapter/batches.go#L76).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Total` | `int` | `total` | `` |  |
| `Completed` | `int` | `completed` | `` |  |
| `Failed` | `int` | `failed` | `` |  |

## internal/adapter.batchInputLine

Source: [internal/adapter/batches.go](../../../internal/adapter/batches.go#L83).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `CustomID` | `string` | `custom_id` | `` |  |
| `Method` | `string` | `method` | `` |  |
| `URL` | `string` | `url` | `` |  |
| `Body` | `json.RawMessage` | `body` | `` |  |

## internal/adapter.batchOutputLine

Source: [internal/adapter/batches.go](../../../internal/adapter/batches.go#L94).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `ID` | `string` | `id` | `` |  |
| `CustomID` | `string` | `custom_id` | `` |  |
| `Response` | `*batchOutputResp` | `response` | `` |  |
| `Error` | `*batchOutputError` | `error` | `` |  |

## internal/adapter.batchOutputResp

Source: [internal/adapter/batches.go](../../../internal/adapter/batches.go#L101).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `StatusCode` | `int` | `status_code` | `` |  |
| `RequestID` | `string` | `request_id` | `` |  |
| `Body` | `json.RawMessage` | `body` | `` |  |

## internal/adapter.batchOutputError

Source: [internal/adapter/batches.go](../../../internal/adapter/batches.go#L107).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Code` | `string` | `code` | `` |  |
| `Message` | `string` | `message` | `` |  |

## internal/adapter.createBatchRequest

Source: [internal/adapter/batches.go](../../../internal/adapter/batches.go#L113).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `InputFileID` | `string` | `input_file_id` | `` |  |
| `Endpoint` | `string` | `endpoint` | `` |  |
| `CompletionWindow` | `string` | `completion_window` | `` |  |
| `Metadata` | `map[string]any` | `metadata` | `` |  |

## internal/adapter.BedrockConverseRequest

Source: [internal/adapter/bedrock.go](../../../internal/adapter/bedrock.go#L27).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Messages` | `[]BedrockMessage` | `messages` | `` |  |
| `System` | `[]BedrockContentBlock` | `system,omitempty` | `` |  |
| `ToolConfig` | `*BedrockToolConfig` | `toolConfig,omitempty` | `` |  |

## internal/adapter.BedrockMessage

Source: [internal/adapter/bedrock.go](../../../internal/adapter/bedrock.go#L33).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Role` | `string` | `role` | `` |  |
| `Content` | `[]BedrockContentBlock` | `content` | `` |  |

## internal/adapter.BedrockContentBlock

Source: [internal/adapter/bedrock.go](../../../internal/adapter/bedrock.go#L38).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Text` | `string` | `text,omitempty` | `` |  |
| `Image` | `*BedrockImageBlock` | `image,omitempty` | `` |  |
| `ToolUse` | `*BedrockToolUse` | `toolUse,omitempty` | `` |  |
| `ToolResult` | `*BedrockToolResult` | `toolResult,omitempty` | `` |  |

## internal/adapter.BedrockImageBlock

Source: [internal/adapter/bedrock.go](../../../internal/adapter/bedrock.go#L45).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Format` | `string` | `format` | `` |  |
| `Source` | `map[string]any` | `source` | `` |  |

## internal/adapter.BedrockToolUse

Source: [internal/adapter/bedrock.go](../../../internal/adapter/bedrock.go#L50).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `ToolUseID` | `string` | `toolUseId` | `` |  |
| `Name` | `string` | `name` | `` |  |
| `Input` | `map[string]any` | `input` | `` |  |

## internal/adapter.BedrockToolResult

Source: [internal/adapter/bedrock.go](../../../internal/adapter/bedrock.go#L56).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `ToolUseID` | `string` | `toolUseId` | `` |  |
| `Content` | `[]BedrockContentBlock` | `content` | `` |  |
| `Status` | `string` | `status,omitempty` | `` |  |

## internal/adapter.BedrockToolConfig

Source: [internal/adapter/bedrock.go](../../../internal/adapter/bedrock.go#L62).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Tools` | `[]BedrockTool` | `tools` | `` |  |

## internal/adapter.BedrockTool

Source: [internal/adapter/bedrock.go](../../../internal/adapter/bedrock.go#L66).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `ToolSpec` | `BedrockToolSpec` | `toolSpec` | `` |  |

## internal/adapter.BedrockToolSpec

Source: [internal/adapter/bedrock.go](../../../internal/adapter/bedrock.go#L70).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Name` | `string` | `name` | `` |  |
| `Description` | `string` | `description,omitempty` | `` |  |
| `InputSchema` | `BedrockInputSchema` | `inputSchema` | `` |  |

## internal/adapter.BedrockInputSchema

Source: [internal/adapter/bedrock.go](../../../internal/adapter/bedrock.go#L76).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `JSON` | `map[string]any` | `json` | `` |  |

## internal/adapter.BedrockConverseResponse

Source: [internal/adapter/bedrock.go](../../../internal/adapter/bedrock.go#L80).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Output` | `BedrockOutput` | `output` | `` |  |
| `StopReason` | `string` | `stopReason` | `` |  |
| `Usage` | `BedrockUsage` | `usage` | `` |  |
| `Metrics` | `BedrockMetrics` | `metrics` | `` |  |

## internal/adapter.BedrockOutput

Source: [internal/adapter/bedrock.go](../../../internal/adapter/bedrock.go#L87).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Message` | `BedrockMessage` | `message` | `` |  |

## internal/adapter.BedrockUsage

Source: [internal/adapter/bedrock.go](../../../internal/adapter/bedrock.go#L90).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `InputTokens` | `int` | `inputTokens` | `` |  |
| `OutputTokens` | `int` | `outputTokens` | `` |  |
| `TotalTokens` | `int` | `totalTokens` | `` |  |

## internal/adapter.BedrockMetrics

Source: [internal/adapter/bedrock.go](../../../internal/adapter/bedrock.go#L95).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `LatencyMS` | `int` | `latencyMs` | `` |  |

## internal/adapter.chromaWrite

Source: [internal/adapter/chroma.go](../../../internal/adapter/chroma.go#L38).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `IDs` | `[]string` | `ids` | `` |  |
| `Embeddings` | `[][]float64` | `embeddings` | `` |  |
| `Metadatas` | `[]map[string]any` | `metadatas` | `` |  |
| `Documents` | `[]*string` | `documents` | `` |  |
| `URIs` | `[]*string` | `uris` | `` |  |

## internal/adapter.chromaGet

Source: [internal/adapter/chroma.go](../../../internal/adapter/chroma.go#L45).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `IDs` | `[]string` | `ids` | `` |  |
| `Where` | `map[string]any` | `where` | `` |  |
| `Limit` | `int` | `limit` | `` |  |
| `Offset` | `int` | `offset` | `` |  |
| `Include` | `[]string` | `include` | `` |  |

## internal/adapter.chromaQuery

Source: [internal/adapter/chroma.go](../../../internal/adapter/chroma.go#L52).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `QueryEmbeddings` | `[][]float64` | `query_embeddings` | `` |  |
| `NResults` | `int` | `n_results` | `` |  |
| `Where` | `map[string]any` | `where` | `` |  |
| `Include` | `[]string` | `include` | `` |  |

## internal/adapter.chromaCreate

Source: [internal/adapter/chroma.go](../../../internal/adapter/chroma.go#L58).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Name` | `string` | `name` | `` |  |
| `GetOrCreate` | `bool` | `get_or_create` | `` |  |
| `Metadata` | `map[string]any` | `metadata` | `` |  |
| `Configuration` | `map[string]any` | `configuration` | `` |  |

## internal/adapter.cohereRerankRequest

Source: [internal/adapter/cohere_rerank.go](../../../internal/adapter/cohere_rerank.go#L24).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Model` | `string` | `model` | `` |  |
| `Query` | `string` | `query` | `` |  |
| `Documents` | `[]string` | `documents` | `` |  |
| `TopN` | `*int` | `top_n,omitempty` | `` |  |

## internal/adapter.cohereRerankResult

Source: [internal/adapter/cohere_rerank.go](../../../internal/adapter/cohere_rerank.go#L30).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Index` | `int` | `index` | `` |  |
| `RelevanceScore` | `float64` | `relevance_score` | `` |  |

## internal/adapter.Conversation

Source: [internal/adapter/conversations.go](../../../internal/adapter/conversations.go#L41).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `ID` | `string` | `id` | `` |  |
| `Object` | `string` | `object` | `` | always "conversation" |
| `CreatedAt` | `int64` | `created_at` | `` |  |
| `Metadata` | `map[string]any` | `metadata` | `` |  |

## internal/adapter.conversationItem

Source: [internal/adapter/conversations.go](../../../internal/adapter/conversations.go#L54).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `ID` | `string` | `id` | `` |  |
| `Type` | `string` | `type` | `` |  |
| `Object` | `string` | `object,omitempty` | `` | "conversation.item" on read |
| `CreatedAt` | `int64` | `created_at,omitempty` | `` |  |
| `Role` | `string` | `role,omitempty` | `` |  |
| `Content` | `json.RawMessage` | `content,omitempty` | `` |  |
| `Status` | `string` | `status,omitempty` | `` |  |
| `CallID` | `string` | `call_id,omitempty` | `` |  |
| `Name` | `string` | `name,omitempty` | `` |  |
| `Arguments` | `string` | `arguments,omitempty` | `` |  |
| `Output` | `json.RawMessage` | `output,omitempty` | `` |  |

## internal/adapter.createConversationRequest

Source: [internal/adapter/conversations.go](../../../internal/adapter/conversations.go#L70).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Metadata` | `map[string]any` | `metadata,omitempty` | `` |  |
| `Items` | `[]conversationItem` | `items,omitempty` | `` |  |

## internal/adapter.updateConversationRequest

Source: [internal/adapter/conversations.go](../../../internal/adapter/conversations.go#L79).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Metadata` | `*map[string]any` | `metadata,omitempty` | `` |  |

## internal/adapter.createItemsRequest

Source: [internal/adapter/conversations.go](../../../internal/adapter/conversations.go#L84).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Items` | `[]conversationItem` | `items` | `` |  |

## internal/adapter.EmbeddingsRequest

Source: [internal/adapter/embeddings.go](../../../internal/adapter/embeddings.go#L28).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Model` | `string` | `model` | `` |  |
| `Input` | `json.RawMessage` | `input` | `` |  |
| `EncodingFormat` | `string` | `encoding_format,omitempty` | `` |  |
| `Dimensions` | `*int` | `dimensions,omitempty` | `` |  |
| `User` | `string` | `user,omitempty` | `` |  |

## internal/adapter.EmbeddingsResponse

Source: [internal/adapter/embeddings.go](../../../internal/adapter/embeddings.go#L37).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Object` | `string` | `object` | `` |  |
| `Data` | `[]EmbeddingData` | `data` | `` |  |
| `Model` | `string` | `model` | `` |  |
| `Usage` | `EmbeddingsUsage` | `usage` | `` |  |

## internal/adapter.EmbeddingData

Source: [internal/adapter/embeddings.go](../../../internal/adapter/embeddings.go#L47).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Object` | `string` | `object` | `` |  |
| `Index` | `int` | `index` | `` |  |
| `Embedding` | `any` | `embedding` | `` |  |

## internal/adapter.EmbeddingsUsage

Source: [internal/adapter/embeddings.go](../../../internal/adapter/embeddings.go#L55).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `PromptTokens` | `int` | `prompt_tokens` | `` |  |
| `TotalTokens` | `int` | `total_tokens` | `` |  |

## internal/adapter.fileObject

Source: [internal/adapter/files.go](../../../internal/adapter/files.go#L140).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `ID` | `string` | `id` | `` |  |
| `Object` | `string` | `object` | `` |  |
| `Bytes` | `int64` | `bytes` | `` |  |
| `CreatedAt` | `int64` | `created_at` | `` |  |
| `Filename` | `string` | `filename` | `` |  |
| `Purpose` | `string` | `purpose` | `` |  |
| `Status` | `string` | `status` | `` |  |

## internal/adapter.GeminiRequest

Source: [internal/adapter/gemini.go](../../../internal/adapter/gemini.go#L20).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Contents` | `[]GeminiContent` | `contents` | `` |  |
| `SystemInstruction` | `*GeminiContent` | `systemInstruction,omitempty` | `` |  |
| `Tools` | `[]GeminiToolDeclaration` | `tools,omitempty` | `` |  |
| `GenerationConfig` | `map[string]any` | `generationConfig,omitempty` | `` |  |
| `ToolConfig` | `*GeminiToolConfig` | `toolConfig,omitempty` | `` | ToolConfig carries functionCallingConfig. NONE is always honored (function calls suppressed — R9-5); ANY forcing and allowedFunctionNames are enforced under the strict-tools knob (round-11). |

## internal/adapter.GeminiToolConfig

Source: [internal/adapter/gemini.go](../../../internal/adapter/gemini.go#L33).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `FunctionCallingConfig` | `*GeminiFunctionCallingConfig` | `functionCallingConfig,omitempty` | `` |  |

## internal/adapter.GeminiFunctionCallingConfig

Source: [internal/adapter/gemini.go](../../../internal/adapter/gemini.go#L39).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Mode` | `string` | `mode,omitempty` | `` |  |
| `AllowedFunctionNames` | `[]string` | `allowedFunctionNames,omitempty` | `` |  |

## internal/adapter.GeminiContent

Source: [internal/adapter/gemini.go](../../../internal/adapter/gemini.go#L46).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Role` | `string` | `role,omitempty` | `` |  |
| `Parts` | `[]GeminiPart` | `parts` | `` |  |

## internal/adapter.GeminiPart

Source: [internal/adapter/gemini.go](../../../internal/adapter/gemini.go#L54).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Text` | `string` | `text,omitempty` | `` |  |
| `FunctionCall` | `*GeminiFunctionCall` | `functionCall,omitempty` | `` |  |
| `FunctionResponse` | `*GeminiFunctionResponse` | `functionResponse,omitempty` | `` |  |
| `InlineData` | `*GeminiInlineData` | `inlineData,omitempty` | `` |  |
| `FileData` | `*GeminiFileData` | `fileData,omitempty` | `` |  |

## internal/adapter.GeminiInlineData

Source: [internal/adapter/gemini.go](../../../internal/adapter/gemini.go#L65).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `MimeType` | `string` | `mimeType,omitempty` | `` |  |
| `Data` | `string` | `data,omitempty` | `` |  |

## internal/adapter.GeminiFileData

Source: [internal/adapter/gemini.go](../../../internal/adapter/gemini.go#L71).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `MimeType` | `string` | `mimeType,omitempty` | `` |  |
| `FileURI` | `string` | `fileUri,omitempty` | `` |  |

## internal/adapter.GeminiFunctionResponse

Source: [internal/adapter/gemini.go](../../../internal/adapter/gemini.go#L97).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Name` | `string` | `name,omitempty` | `` |  |
| `Response` | `map[string]any` | `response,omitempty` | `` |  |

## internal/adapter.GeminiToolDeclaration

Source: [internal/adapter/gemini.go](../../../internal/adapter/gemini.go#L103).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `FunctionDeclarations` | `[]GeminiFunctionDecl` | `functionDeclarations,omitempty` | `` |  |

## internal/adapter.GeminiFunctionDecl

Source: [internal/adapter/gemini.go](../../../internal/adapter/gemini.go#L108).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Name` | `string` | `name` | `` |  |
| `Description` | `string` | `description,omitempty` | `` |  |
| `Parameters` | `map[string]any` | `parameters,omitempty` | `` |  |

## internal/adapter.GeminiFunctionCall

Source: [internal/adapter/gemini.go](../../../internal/adapter/gemini.go#L117).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Name` | `string` | `name` | `` |  |
| `Args` | `map[string]any` | `args,omitzero` | `` |  |

## internal/adapter.GeminiResponse

Source: [internal/adapter/gemini.go](../../../internal/adapter/gemini.go#L125).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Candidates` | `[]GeminiCandidate` | `candidates` | `` |  |
| `UsageMetadata` | `GeminiUsageMetadata` | `usageMetadata` | `` |  |
| `ModelVersion` | `string` | `modelVersion,omitempty` | `` |  |

## internal/adapter.GeminiCandidate

Source: [internal/adapter/gemini.go](../../../internal/adapter/gemini.go#L132).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Content` | `GeminiContent` | `content` | `` |  |
| `FinishReason` | `string` | `finishReason` | `` |  |
| `Index` | `int` | `index` | `` |  |

## internal/adapter.GeminiUsageMetadata

Source: [internal/adapter/gemini.go](../../../internal/adapter/gemini.go#L139).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `PromptTokenCount` | `int` | `promptTokenCount` | `` |  |
| `CandidatesTokenCount` | `int` | `candidatesTokenCount` | `` |  |
| `TotalTokenCount` | `int` | `totalTokenCount` | `` |  |

## internal/adapter.geminiError

Source: [internal/adapter/gemini.go](../../../internal/adapter/gemini.go#L447).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Error` | `geminiErrorBody` | `error` | `` |  |

## internal/adapter.geminiErrorBody

Source: [internal/adapter/gemini.go](../../../internal/adapter/gemini.go#L451).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Code` | `int` | `code` | `` |  |
| `Message` | `string` | `message` | `` |  |
| `Status` | `string` | `status` | `` |  |

## internal/adapter.ModerationsRequest

Source: [internal/adapter/moderations.go](../../../internal/adapter/moderations.go#L72).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Input` | `json.RawMessage` | `input` | `` |  |
| `Model` | `string` | `model,omitempty` | `` |  |

## internal/adapter.ModerationsResponse

Source: [internal/adapter/moderations.go](../../../internal/adapter/moderations.go#L78).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `ID` | `string` | `id` | `` |  |
| `Model` | `string` | `model` | `` |  |
| `Results` | `[]ModerationResult` | `results` | `` |  |

## internal/adapter.ModerationResult

Source: [internal/adapter/moderations.go](../../../internal/adapter/moderations.go#L88).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Flagged` | `bool` | `flagged` | `` |  |
| `Categories` | `map[string]bool` | `categories` | `` |  |
| `CategoryScores` | `map[string]float64` | `category_scores` | `` |  |
| `CategoryAppliedInputTypes` | `map[string][]string` | `category_applied_input_types` | `` |  |

## internal/adapter.moderationInputPart

Source: [internal/adapter/moderations.go](../../../internal/adapter/moderations.go#L167).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Type` | `string` | `type` | `` |  |
| `Text` | `string` | `text` | `` |  |

## internal/adapter.OllamaChatRequest

Source: [internal/adapter/ollama.go](../../../internal/adapter/ollama.go#L26).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Model` | `string` | `model` | `` |  |
| `Messages` | `[]OllamaMessage` | `messages` | `` |  |
| `Tools` | `[]OpenAITool` | `tools,omitempty` | `` |  |
| `Stream` | `*bool` | `stream,omitempty` | `` |  |

## internal/adapter.OllamaMessage

Source: [internal/adapter/ollama.go](../../../internal/adapter/ollama.go#L33).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Role` | `string` | `role` | `` |  |
| `Content` | `string` | `content` | `` |  |
| `Thinking` | `string` | `thinking,omitempty` | `` |  |
| `Images` | `[]string` | `images,omitempty` | `` |  |
| `ToolCalls` | `[]OllamaToolCall` | `tool_calls,omitempty` | `` |  |

## internal/adapter.OllamaToolCall

Source: [internal/adapter/ollama.go](../../../internal/adapter/ollama.go#L41).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Function` | `OllamaFunctionCall` | `function` | `` |  |

## internal/adapter.OllamaFunctionCall

Source: [internal/adapter/ollama.go](../../../internal/adapter/ollama.go#L45).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Name` | `string` | `name` | `` |  |
| `Description` | `string` | `description,omitempty` | `` |  |
| `Arguments` | `map[string]any` | `arguments` | `` |  |

## internal/adapter.OllamaChatResponse

Source: [internal/adapter/ollama.go](../../../internal/adapter/ollama.go#L51).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Model` | `string` | `model` | `` |  |
| `CreatedAt` | `string` | `created_at` | `` |  |
| `Message` | `OllamaMessage` | `message` | `` |  |
| `Done` | `bool` | `done` | `` |  |
| `DoneReason` | `string` | `done_reason,omitempty` | `` |  |
| `TotalDuration` | `int64` | `total_duration,omitempty` | `` |  |
| `LoadDuration` | `int64` | `load_duration,omitempty` | `` |  |
| `PromptEvalCount` | `int` | `prompt_eval_count,omitempty` | `` |  |
| `PromptEvalDuration` | `int64` | `prompt_eval_duration,omitempty` | `` |  |
| `EvalCount` | `int` | `eval_count,omitempty` | `` |  |
| `EvalDuration` | `int64` | `eval_duration,omitempty` | `` |  |

## internal/adapter.ChatCompletionRequest

Source: [internal/adapter/openai.go](../../../internal/adapter/openai.go#L20).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Model` | `string` | `model` | `` |  |
| `Messages` | `[]OpenAIMessage` | `messages` | `` |  |
| `Tools` | `[]OpenAITool` | `tools,omitempty` | `` |  |
| `ToolChoice` | `any` | `tool_choice,omitempty` | `` |  |
| `ParallelToolCalls` | `*bool` | `parallel_tool_calls,omitempty` | `` | ParallelToolCalls false caps the response to at most one tool call (round-11; previously the field was silently swallowed). |
| `Stream` | `bool` | `stream,omitempty` | `` |  |
| `Temperature` | `*float64` | `temperature,omitempty` | `` |  |
| `MaxTokens` | `*int` | `max_tokens,omitempty` | `` |  |
| `ResponseFormat` | `*ResponseFormat` | `response_format,omitempty` | `` |  |
| `StreamOptions` | `*StreamOptions` | `stream_options,omitempty` | `` | StreamOptions carries include_usage — the final streaming chunk then reports usage (round-9 R9-9). |

## internal/adapter.StreamOptions

Source: [internal/adapter/openai.go](../../../internal/adapter/openai.go#L38).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `IncludeUsage` | `bool` | `include_usage` | `` |  |

## internal/adapter.OpenAIMessage

Source: [internal/adapter/openai.go](../../../internal/adapter/openai.go#L43).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Role` | `string` | `role` | `` |  |
| `Content` | `any` | `content` | `` |  |
| `ToolCalls` | `[]OpenAIToolCall` | `tool_calls,omitempty` | `` |  |
| `ToolCallID` | `string` | `tool_call_id,omitempty` | `` |  |

## internal/adapter.OpenAITool

Source: [internal/adapter/openai.go](../../../internal/adapter/openai.go#L51).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Type` | `string` | `type` | `` |  |
| `Function` | `OpenAIFunction` | `function` | `` |  |

## internal/adapter.OpenAIFunction

Source: [internal/adapter/openai.go](../../../internal/adapter/openai.go#L57).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Name` | `string` | `name` | `` |  |
| `Description` | `string` | `description,omitempty` | `` |  |
| `Parameters` | `map[string]any` | `parameters,omitempty` | `` |  |
| `Strict` | `*bool` | `strict,omitempty` | `` | Strict opts the function into structured-outputs schema validation — the real API validates the schema SHAPE at request time (round-11). |

## internal/adapter.OpenAIToolCall

Source: [internal/adapter/openai.go](../../../internal/adapter/openai.go#L67).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `ID` | `string` | `id` | `` |  |
| `Type` | `string` | `type` | `` |  |
| `Function` | `OpenAIFunctionCall` | `function` | `` |  |

## internal/adapter.OpenAIFunctionCall

Source: [internal/adapter/openai.go](../../../internal/adapter/openai.go#L74).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Name` | `string` | `name` | `` |  |
| `Arguments` | `string` | `arguments` | `` |  |

## internal/adapter.ChatCompletionResponse

Source: [internal/adapter/openai.go](../../../internal/adapter/openai.go#L82).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `ID` | `string` | `id` | `` |  |
| `Object` | `string` | `object` | `` |  |
| `Created` | `int64` | `created` | `` |  |
| `Model` | `string` | `model` | `` |  |
| `Choices` | `[]ChatCompletionChoice` | `choices` | `` |  |
| `Usage` | `OpenAIUsage` | `usage` | `` |  |

## internal/adapter.ChatCompletionChoice

Source: [internal/adapter/openai.go](../../../internal/adapter/openai.go#L92).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Index` | `int` | `index` | `` |  |
| `Message` | `OpenAIResponseMessage` | `message` | `` |  |
| `FinishReason` | `string` | `finish_reason` | `` |  |

## internal/adapter.OpenAIResponseMessage

Source: [internal/adapter/openai.go](../../../internal/adapter/openai.go#L99).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Role` | `string` | `role` | `` |  |
| `Content` | `*string` | `content` | `` |  |
| `ToolCalls` | `[]OpenAIToolCall` | `tool_calls,omitempty` | `` |  |
| `Refusal` | `*string` | `refusal,omitempty` | `` | Refusal is OpenAI's structured refusal field (FB-03 semantic errors); omitted unless the scenario plants one. |

## internal/adapter.OpenAIUsage

Source: [internal/adapter/openai.go](../../../internal/adapter/openai.go#L109).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `PromptTokens` | `int` | `prompt_tokens` | `` |  |
| `CompletionTokens` | `int` | `completion_tokens` | `` |  |
| `TotalTokens` | `int` | `total_tokens` | `` |  |

## internal/adapter.openAIError

Source: [internal/adapter/openai.go](../../../internal/adapter/openai.go#L529).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Error` | `openAIErrorBody` | `error` | `` |  |

## internal/adapter.openAIErrorBody

Source: [internal/adapter/openai.go](../../../internal/adapter/openai.go#L533).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Type` | `string` | `type` | `` |  |
| `Message` | `string` | `message` | `` |  |
| `Param` | `any` | `param` | `` | Param is always rendered (null when no specific field is at fault) — the real error envelope carries it on every error (round-9 R9-14). |
| `Code` | `string` | `code,omitempty` | `` | Code is OpenAI's stable machine-readable error code (e.g. invalid_api_key, rate_limit_exceeded). Omitted when empty so non-chaos errors are unchanged. |

## internal/adapter.pineconeVector

Source: [internal/adapter/pinecone.go](../../../internal/adapter/pinecone.go#L36).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `ID` | `string` | `id` | `` |  |
| `Values` | `[]float64` | `values` | `` |  |
| `Metadata` | `map[string]any` | `metadata,omitempty` | `` |  |

## internal/adapter.pineconeUpsertRequest

Source: [internal/adapter/pinecone.go](../../../internal/adapter/pinecone.go#L41).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Vectors` | `[]pineconeVector` | `vectors` | `` |  |
| `Namespace` | `string` | `namespace,omitempty` | `` |  |

## internal/adapter.pineconeQueryRequest

Source: [internal/adapter/pinecone.go](../../../internal/adapter/pinecone.go#L45).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Vector` | `[]float64` | `vector` | `` |  |
| `TopK` | `int` | `topK` | `` |  |
| `Namespace` | `string` | `namespace,omitempty` | `` |  |
| `Filter` | `map[string]any` | `filter,omitempty` | `` |  |
| `IncludeValues` | `bool` | `includeValues,omitempty` | `` |  |
| `IncludeMetadata` | `bool` | `includeMetadata,omitempty` | `` |  |

## internal/adapter.pineconeDeleteRequest

Source: [internal/adapter/pinecone.go](../../../internal/adapter/pinecone.go#L53).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `IDs` | `[]string` | `ids,omitempty` | `` |  |
| `DeleteAll` | `bool` | `deleteAll,omitempty` | `` |  |
| `Namespace` | `string` | `namespace,omitempty` | `` |  |

## internal/adapter.qdrantVectorsConfig

Source: [internal/adapter/qdrant.go](../../../internal/adapter/qdrant.go#L43).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Size` | `int` | `size` | `` |  |
| `Distance` | `string` | `distance` | `` |  |

## internal/adapter.qdrantCreateRequest

Source: [internal/adapter/qdrant.go](../../../internal/adapter/qdrant.go#L48).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Vectors` | `qdrantVectorsConfig` | `vectors` | `` |  |

## internal/adapter.qdrantPoint

Source: [internal/adapter/qdrant.go](../../../internal/adapter/qdrant.go#L52).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `ID` | `any` | `id` | `` |  |
| `Vector` | `[]float64` | `vector` | `` |  |
| `Payload` | `map[string]any` | `payload,omitempty` | `` |  |

## internal/adapter.qdrantPointsRequest

Source: [internal/adapter/qdrant.go](../../../internal/adapter/qdrant.go#L58).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Points` | `[]qdrantPoint` | `points` | `` |  |

## internal/adapter.qdrantIDsRequest

Source: [internal/adapter/qdrant.go](../../../internal/adapter/qdrant.go#L62).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Points` | `[]any` | `points` | `` |  |

## internal/adapter.qdrantSearchRequest

Source: [internal/adapter/qdrant.go](../../../internal/adapter/qdrant.go#L66).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Vector` | `[]float64` | `vector` | `` |  |
| `Limit` | `int` | `limit` | `` |  |
| `ScoreThreshold` | `*float64` | `score_threshold,omitempty` | `` |  |
| `Filter` | `map[string]any` | `filter,omitempty` | `` |  |
| `WithPayload` | `*bool` | `with_payload,omitempty` | `` |  |

## internal/adapter.ResponsesRequest

Source: [internal/adapter/responses.go](../../../internal/adapter/responses.go#L31).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Model` | `string` | `model` | `` |  |
| `Input` | `json.RawMessage` | `input` | `` |  |
| `Instructions` | `*string` | `instructions,omitempty` | `` |  |
| `Tools` | `[]json.RawMessage` | `tools,omitempty` | `` |  |
| `ToolChoice` | `json.RawMessage` | `tool_choice,omitempty` | `` |  |
| `PreviousResponseID` | `*string` | `previous_response_id,omitempty` | `` |  |
| `Stream` | `bool` | `stream,omitempty` | `` |  |
| `Temperature` | `*float64` | `temperature,omitempty` | `` |  |
| `TopP` | `*float64` | `top_p,omitempty` | `` |  |
| `MaxOutputTokens` | `*int` | `max_output_tokens,omitempty` | `` |  |
| `Metadata` | `map[string]any` | `metadata,omitempty` | `` |  |
| `Store` | `*bool` | `store,omitempty` | `` |  |
| `Conversation` | `json.RawMessage` | `conversation,omitempty` | `` | Conversation references an OpenAI Conversation (NF-02): a string id or an object {"id": "..."}. Its stored Items are replayed as prior turns, and this turn's input + output are appended regardless of store. It is mutually exclusive with previous_response_id. |
| `ParallelToolCalls` | `*bool` | `parallel_tool_calls,omitempty` | `` |  |
| `Text` | `json.RawMessage` | `text,omitempty` | `` |  |
| `Reasoning` | `json.RawMessage` | `reasoning,omitempty` | `` |  |
| `User` | `*string` | `user,omitempty` | `` |  |

## internal/adapter.responsesInputItem

Source: [internal/adapter/responses.go](../../../internal/adapter/responses.go#L61).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Type` | `string` | `type` | `` |  |
| `Role` | `string` | `role` | `` |  |
| `Content` | `json.RawMessage` | `content` | `` |  |
| `CallID` | `string` | `call_id` | `` |  |
| `Output` | `json.RawMessage` | `output` | `` |  |
| `Name` | `string` | `name` | `` |  |
| `Arguments` | `string` | `arguments` | `` | echoed function_call args (fingerprint material) |

## internal/adapter.ResponsesResponse

Source: [internal/adapter/responses.go](../../../internal/adapter/responses.go#L76).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `ID` | `string` | `id` | `` |  |
| `Object` | `string` | `object` | `` |  |
| `CreatedAt` | `int64` | `created_at` | `` |  |
| `Status` | `string` | `status` | `` |  |
| `Error` | `any` | `error` | `` |  |
| `IncompleteDetails` | `any` | `incomplete_details` | `` |  |
| `Instructions` | `any` | `instructions` | `` |  |
| `MaxOutputTokens` | `any` | `max_output_tokens` | `` |  |
| `Model` | `string` | `model` | `` |  |
| `Output` | `[]any` | `output` | `` |  |
| `ParallelToolCalls` | `bool` | `parallel_tool_calls` | `` |  |
| `PreviousResponseID` | `any` | `previous_response_id` | `` |  |
| `Reasoning` | `any` | `reasoning` | `` |  |
| `Store` | `bool` | `store` | `` |  |
| `Conversation` | `any` | `conversation` | `` | Conversation echoes the referenced conversation as {"id": …} (NF-02), or null. SDKs read response.conversation.id to continue a thread. |
| `Temperature` | `float64` | `temperature` | `` |  |
| `Text` | `any` | `text` | `` |  |
| `ToolChoice` | `any` | `tool_choice` | `` |  |
| `Tools` | `[]any` | `tools` | `` |  |
| `TopP` | `float64` | `top_p` | `` |  |
| `Truncation` | `string` | `truncation` | `` |  |
| `Usage` | `ResponsesUsage` | `usage` | `` |  |
| `User` | `any` | `user` | `` |  |
| `Metadata` | `map[string]any` | `metadata` | `` |  |

## internal/adapter.ResponsesUsage

Source: [internal/adapter/responses.go](../../../internal/adapter/responses.go#L108).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `InputTokens` | `int` | `input_tokens` | `` |  |
| `InputTokensDetails` | `responsesInputTokenDetail` | `input_tokens_details` | `` |  |
| `OutputTokens` | `int` | `output_tokens` | `` |  |
| `OutputTokensDetails` | `responsesOutputTokenDetail` | `output_tokens_details` | `` |  |
| `TotalTokens` | `int` | `total_tokens` | `` |  |

## internal/adapter.responsesInputTokenDetail

Source: [internal/adapter/responses.go](../../../internal/adapter/responses.go#L116).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `CachedTokens` | `int` | `cached_tokens` | `` |  |

## internal/adapter.responsesOutputTokenDetail

Source: [internal/adapter/responses.go](../../../internal/adapter/responses.go#L120).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `ReasoningTokens` | `int` | `reasoning_tokens` | `` |  |

## internal/adapter.responseMessageItem

Source: [internal/adapter/responses.go](../../../internal/adapter/responses.go#L126).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Type` | `string` | `type` | `` |  |
| `ID` | `string` | `id` | `` |  |
| `Status` | `string` | `status` | `` |  |
| `Role` | `string` | `role` | `` |  |
| `Content` | `[]any` | `content` | `` |  |

## internal/adapter.responseOutputText

Source: [internal/adapter/responses.go](../../../internal/adapter/responses.go#L135).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Type` | `string` | `type` | `` |  |
| `Text` | `string` | `text` | `` |  |
| `Annotations` | `[]any` | `annotations` | `` |  |

## internal/adapter.responseRefusalPart

Source: [internal/adapter/responses.go](../../../internal/adapter/responses.go#L142).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Type` | `string` | `type` | `` |  |
| `Refusal` | `string` | `refusal` | `` |  |

## internal/adapter.responseFunctionCallItem

Source: [internal/adapter/responses.go](../../../internal/adapter/responses.go#L148).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Type` | `string` | `type` | `` |  |
| `ID` | `string` | `id` | `` |  |
| `CallID` | `string` | `call_id` | `` |  |
| `Name` | `string` | `name` | `` |  |
| `Arguments` | `string` | `arguments` | `` |  |
| `Status` | `string` | `status` | `` |  |

## internal/adapter.ResponseFormat

Source: [internal/adapter/structured_output.go](../../../internal/adapter/structured_output.go#L16).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Type` | `string` | `type` | `` |  |
| `JSONSchema` | `*ResponseFormatJSON` | `json_schema,omitempty` | `` |  |

## internal/adapter.ResponseFormatJSON

Source: [internal/adapter/structured_output.go](../../../internal/adapter/structured_output.go#L22).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Name` | `string` | `name,omitempty` | `` |  |
| `Schema` | `map[string]any` | `schema,omitempty` | `` |  |
| `Strict` | `*bool` | `strict,omitempty` | `` |  |

## internal/adapter.tavilySearchRequest

Source: [internal/adapter/tavily_search.go](../../../internal/adapter/tavily_search.go#L59).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Query` | `string` | `query` | `` |  |
| `SearchDepth` | `string` | `search_depth,omitempty` | `` |  |
| `Topic` | `string` | `topic,omitempty` | `` |  |
| `MaxResults` | `int` | `max_results,omitempty` | `` |  |
| `IncludeAnswer` | `any` | `include_answer,omitempty` | `` |  |
| `IncludeRawContent` | `any` | `include_raw_content,omitempty` | `` |  |
| `IncludeDomains` | `[]string` | `include_domains,omitempty` | `` |  |
| `ExcludeDomains` | `[]string` | `exclude_domains,omitempty` | `` |  |
| `TimeRange` | `string` | `time_range,omitempty` | `` |  |
| `StartDate` | `string` | `start_date,omitempty` | `` |  |
| `EndDate` | `string` | `end_date,omitempty` | `` |  |

## internal/audit.Actor

Source: [internal/audit/types.go](../../../internal/audit/types.go#L78).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Name` | `string` | `name` | `` |  |
| `TenantID` | `string` | `tenant_id,omitempty` | `` |  |
| `KeyID` | `string` | `key_id,omitempty` | `` |  |
| `Role` | `string` | `role,omitempty` | `` |  |
| `RemoteIP` | `string` | `remote_ip,omitempty` | `` |  |

## internal/audit.Event

Source: [internal/audit/types.go](../../../internal/audit/types.go#L93).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `ID` | `int64` | `id` | `` |  |
| `Timestamp` | `time.Time` | `timestamp` | `` |  |
| `Kind` | `EventKind` | `kind` | `` |  |
| `Actor` | `Actor` | `actor` | `` |  |
| `Target` | `string` | `target` | `` |  |
| `Details` | `string` | `details,omitempty` | `` |  |

## internal/config.ValidateReport

Source: [internal/config/validate_bytes.go](../../../internal/config/validate_bytes.go#L19).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Kind` | `string` | `kind` | `` | Kind is the top-level `kind:` value from the document. Empty when the document failed to parse at all. |
| `Errors` | `[]*ValidationError` | `errors` | `` | Errors is the ordered list of validation problems. Line and column are populated when the underlying yaml.Node tree provided a location. |
| `Warnings` | `[]*ValidationError` | `warnings,omitempty` | `` | Warnings are non-fatal lint findings (round-11): configuration that loads but silently does nothing on this agent's protocol. |

## internal/config.ValidationError

Source: [internal/config/validator.go](../../../internal/config/validator.go#L21).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `File` | `string` | `file` | `` |  |
| `Line` | `int` | `line,omitempty` | `` |  |
| `Column` | `int` | `column,omitempty` | `` |  |
| `Field` | `string` | `field` | `` |  |
| `Message` | `string` | `message` | `` |  |
| `Suggestion` | `string` | `suggestion,omitempty` | `` |  |

## internal/contract.Contract

Source: [internal/contract/contract.go](../../../internal/contract/contract.go#L20).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Name` | `string` | `name` | `` |  |
| `Protocol` | `string` | `protocol` | `` |  |
| `Model` | `string` | `model,omitempty` | `` |  |
| `Tools` | `[]ToolContract` | `tools,omitempty` | `` |  |
| `Scenarios` | `[]string` | `scenarios,omitempty` | `` | Scenarios records the ordered list of scenario names the agent advertises; it is part of the contract because consumers may assert on matched scenario names in tests. |
| `Streaming` | `*StreamContract` | `streaming,omitempty` | `` |  |

## internal/contract.ToolContract

Source: [internal/contract/contract.go](../../../internal/contract/contract.go#L35).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Name` | `string` | `name` | `` |  |
| `Description` | `string` | `description,omitempty` | `` |  |
| `Parameters` | `map[string]interface{}` | `parameters,omitempty` | `` |  |

## internal/contract.StreamContract

Source: [internal/contract/contract.go](../../../internal/contract/contract.go#L42).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Enabled` | `bool` | `enabled` | `` |  |

## internal/contract.Change

Source: [internal/contract/contract.go](../../../internal/contract/contract.go#L122).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Severity` | `Severity` | `severity` | `` |  |
| `Path` | `string` | `path` | `` |  |
| `Message` | `string` | `message` | `` |  |

## internal/conversion.aimockDocument

Source: [internal/conversion/aimock.go](../../../internal/conversion/aimock.go#L30).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Fixtures` | `[]aimockFixture` | `fixtures` | `` |  |

## internal/conversion.aimockFixture

Source: [internal/conversion/aimock.go](../../../internal/conversion/aimock.go#L34).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Match` | `map[string]json.RawMessage` | `match` | `` |  |
| `Response` | `aimockResponse` | `response` | `` |  |

## internal/conversion.aimockResponse

Source: [internal/conversion/aimock.go](../../../internal/conversion/aimock.go#L39).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Content` | `json.RawMessage` | `content` | `` |  |
| `ToolCalls` | `[]aimockToolCall` | `toolCalls` | `` |  |
| `FinishReason` | `string` | `finishReason` | `` |  |
| `Refusal` | `string` | `refusal` | `` |  |

## internal/conversion.aimockToolCall

Source: [internal/conversion/aimock.go](../../../internal/conversion/aimock.go#L46).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Name` | `string` | `name` | `` |  |
| `Arguments` | `map[string]any` | `arguments` | `` |  |

## internal/drift.Baseline

Source: [internal/drift/baseline.go](../../../internal/drift/baseline.go#L13).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Version` | `string` | `version` | `` |  |
| `Name` | `string` | `name` | `` |  |
| `Revision` | `int` | `revision` | `` |  |
| `Operation` | `string` | `operation` | `` |  |
| `Adapter` | `string` | `adapter,omitempty` | `` |  |
| `SDK` | `string` | `sdk` | `` |  |
| `Provider` | `string` | `provider` | `` |  |
| `Mock` | `string` | `mock` | `` |  |
| `IgnorePaths` | `[]string` | `ignore_paths,omitempty` | `` |  |

## internal/drift.BaselineReference

Source: [internal/drift/baseline.go](../../../internal/drift/baseline.go#L25).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Name` | `string` | `name` | `` |  |
| `Revision` | `int` | `revision` | `` |  |

## internal/drift.Shape

Source: [internal/drift/drift.go](../../../internal/drift/drift.go#L77).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Types` | `[]string` | `types` | `` |  |
| `Nullable` | `bool` | `nullable,omitempty` | `` |  |

## internal/drift.Finding

Source: [internal/drift/drift.go](../../../internal/drift/drift.go#L82).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Operation` | `string` | `operation` | `` |  |
| `Path` | `string` | `path` | `` |  |
| `Severity` | `Severity` | `severity` | `` |  |
| `Rule` | `string` | `rule` | `` |  |
| `SDK` | `*Shape` | `sdk,omitempty` | `` |  |
| `Provider` | `*Shape` | `provider,omitempty` | `` |  |
| `Mock` | `*Shape` | `mock,omitempty` | `` |  |
| `Values` | `[]string` | `values,omitempty` | `` |  |
| `SDKValues` | `[]string` | `sdk_values,omitempty` | `` |  |
| `ProviderValues` | `[]string` | `provider_values,omitempty` | `` |  |
| `MockValues` | `[]string` | `mock_values,omitempty` | `` |  |

## internal/drift.Report

Source: [internal/drift/drift.go](../../../internal/drift/drift.go#L96).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Version` | `string` | `version` | `` |  |
| `Operation` | `string` | `operation` | `` |  |
| `Adapter` | `string` | `adapter,omitempty` | `` |  |
| `Baseline` | `*BaselineReference` | `baseline,omitempty` | `` |  |
| `Findings` | `[]Finding` | `findings` | `` |  |
| `Exceptions` | `[]Exception` | `applied_exceptions,omitempty` | `` |  |

## internal/drift.ErrorCase

Source: [internal/drift/errors.go](../../../internal/drift/errors.go#L11).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Status` | `int` | `status` | `` |  |
| `Code` | `string` | `code` | `` |  |
| `Body` | `json.RawMessage` | `body` | `` |  |

## internal/drift.Exception

Source: [internal/drift/exceptions.go](../../../internal/drift/exceptions.go#L14).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Operation` | `string` | `operation` | `` |  |
| `Path` | `string` | `path` | `` |  |
| `Rule` | `string` | `rule` | `` |  |
| `Owner` | `string` | `owner` | `` |  |
| `Expires` | `string` | `expires` | `` |  |

## internal/drift.exceptionFile

Source: [internal/drift/exceptions.go](../../../internal/drift/exceptions.go#L22).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Version` | `string` | `version` | `` |  |
| `Exceptions` | `[]Exception` | `exceptions` | `` |  |

## internal/engine.InboundRequest

Source: [internal/engine/engine.go](../../../internal/engine/engine.go#L24).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `AgentName` | `string` | `agent_name,omitempty` | `` |  |
| `Model` | `string` | `model,omitempty` | `` |  |
| `SessionID` | `string` | `session_id` | `` |  |
| `Messages` | `[]RequestMessage` | `messages` | `` |  |
| `Stream` | `bool` | `stream,omitempty` | `` |  |
| `ToolChoice` | `ToolChoice` | `tool_choice,omitzero` | `` | ToolChoice is the provider-neutral tool_choice contract, populated by each adapter from its wire spelling (round-11 widened round-9's none-only flag). None is always honored (R9-5); Required/Name/ AllowedNames/ParallelDisabled are enforced only under the strict-tools knob (see StrictToolsFor). |
| `RequestToolNames` | `[]string` | `request_tool_names,omitempty` | `` | RequestToolNames are the tool/function names the REQUEST declared — what a real API validates a named tool_choice against (round-11). |
| `StrictFunctions` | `[]StrictFunction` | `strict_functions,omitempty` | `` | StrictFunctions are the request's function tools declared with strict:true. The real API validates their schemas against the structured-outputs subset at request time; the schemas dimension of the strict-tools knob mirrors that (round-11 R9-16b). |

## internal/engine.StrictFunction

Source: [internal/engine/engine.go](../../../internal/engine/engine.go#L48).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Index` | `int` | `index` | `` |  |
| `Name` | `string` | `name` | `` |  |
| `Parameters` | `map[string]any` | `parameters,omitempty` | `` |  |

## internal/engine.ToolChoice

Source: [internal/engine/engine.go](../../../internal/engine/engine.go#L59).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `None` | `bool` | `none,omitempty` | `` | None: the client explicitly forbade tool calls. |
| `Required` | `bool` | `required,omitempty` | `` | Required: the model MUST call at least one tool (OpenAI "required", Anthropic "any", Gemini ANY). Also set when Name forces a specific one. |
| `Name` | `string` | `name,omitempty` | `` | Name forces one specific tool (OpenAI named function, Anthropic {type:"tool"}, a single-entry allowedFunctionNames). |
| `AllowedNames` | `[]string` | `allowed_names,omitempty` | `` | AllowedNames limits which tools may be called (Gemini allowedFunctionNames under mode ANY). |
| `ParallelDisabled` | `bool` | `parallel_disabled,omitempty` | `` | ParallelDisabled caps the response to at most one tool call (parallel_tool_calls:false / disable_parallel_tool_use:true). |

## internal/engine.RequestMessage

Source: [internal/engine/engine.go](../../../internal/engine/engine.go#L77).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Role` | `string` | `role` | `` |  |
| `Content` | `string` | `content` | `` |  |
| `ImageCount` | `int` | `image_count,omitempty` | `` | ImageCount is the number of image content parts the message carried (A-05). It is carried out-of-band so the flattened Content text stays pure (no markers leaking into regex matching, templates, or token counts). |
| `IsToolResult` | `bool` | `is_tool_result,omitempty` | `` | IsToolResult marks a message that carries a tool result back to the model (role:"tool", an Anthropic tool_result block, a Gemini functionResponse part, a Responses function_call_output item). Carried out-of-band so each adapter's matchable-text flattening stays untouched — the tool-loop convergence guard keys on it (round-9). |
| `ToolCalls` | `[]EchoedToolCall` | `tool_calls,omitempty` | `` | ToolCalls carries the tool calls an assistant message ECHOED BACK in the request history — the fingerprint material the convergence guard compares the newly generated calls against. |
| `ToolResultIDs` | `[]string` | `tool_result_ids,omitempty` | `` | ToolResultIDs are the tool-call ids this tool-result message references (tool_call_id / tool_use_id / call_id; the function NAME on Gemini, which has no ids). Strict-mode round-trip validation matches them against prior EchoedToolCall.IDs (round-11 R9-15). |

## internal/engine.EchoedToolCall

Source: [internal/engine/engine.go](../../../internal/engine/engine.go#L105).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `ID` | `string` | `id,omitempty` | `` | ID is the wire id the call was echoed under (call_/toolu_; the function NAME on Gemini, which addresses calls by name). Strict-mode round-trip validation matches tool results against these (round-11). |
| `Name` | `string` | `name` | `` |  |
| `Arguments` | `map[string]any` | `arguments,omitempty` | `` |  |
| `RawArguments` | `string` | `raw_arguments,omitempty` | `` |  |

## internal/engine.NodeResult

Source: [internal/engine/pipeline.go](../../../internal/engine/pipeline.go#L70).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `NodeID` | `string` | `node_id` | `` |  |
| `AgentName` | `string` | `agent_name` | `` |  |
| `Response` | `*Response` | `response` | `` |  |
| `Latency` | `time.Duration` | `latency` | `` |  |

## internal/engine.PipelineResult

Source: [internal/engine/pipeline.go](../../../internal/engine/pipeline.go#L78).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `PipelineName` | `string` | `pipeline_name` | `` |  |
| `Topology` | `string` | `topology` | `` |  |
| `Nodes` | `[]*NodeResult` | `nodes` | `` |  |
| `Latency` | `time.Duration` | `latency` | `` |  |

## internal/engine.Response

Source: [internal/engine/response_generator.go](../../../internal/engine/response_generator.go#L21).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `AgentName` | `string` | `agent_name` | `` |  |
| `Model` | `string` | `model` | `` |  |
| `Content` | `string` | `content` | `` |  |
| `ToolCalls` | `[]types.ToolCallSpec` | `tool_calls,omitempty` | `` |  |
| `ToolResults` | `[]ToolCallResult` | `tool_results,omitempty` | `` |  |
| `ScenarioName` | `string` | `scenario_name` | `` |  |
| `SystemPrompt` | `string` | `system_prompt,omitempty` | `` |  |
| `Metadata` | `map[string]any` | `metadata,omitempty` | `` |  |
| `FinishReason` | `string` | `finish_reason,omitempty` | `` | FinishReason / Refusal carry the scenario's semantic-error overrides (FB-03): a forced finish/stop reason (e.g. "length") and an assistant refusal. Adapters render them per provider. |
| `Refusal` | `string` | `refusal,omitempty` | `` |  |
| `Hallucination` | `*types.HallucinationSpec` | `hallucination,omitempty` | `` | Hallucination, when set, marks this response as a planted hallucination fixture (FB-02). Adapters advertise it via the X-Mockagents-Hallucination response header. |
| `StrictWarnings` | `[]string` | `strict_warnings,omitempty` | `` | StrictWarnings carries strict-tools violations detected in WARN mode (round-11). Adapters surface them via the X-Mockagents-Strict-Violation response header; the request succeeds. |

## internal/engine/state.Message

Source: [internal/engine/state/session.go](../../../internal/engine/state/session.go#L9).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Role` | `string` | `role` | `` |  |
| `Content` | `string` | `content` | `` |  |
| `ToolCalls` | `[]ToolCallMsg` | `tool_calls,omitempty` | `` |  |
| `Timestamp` | `time.Time` | `timestamp` | `` |  |

## internal/engine/state.ToolCallMsg

Source: [internal/engine/state/session.go](../../../internal/engine/state/session.go#L17).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Name` | `string` | `name` | `` |  |
| `Arguments` | `map[string]any` | `arguments,omitempty` | `` |  |

## internal/engine/state.Session

Source: [internal/engine/state/session.go](../../../internal/engine/state/session.go#L23).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `ID` | `string` | `id` | `` |  |
| `AgentName` | `string` | `agent_name` | `` |  |
| `Messages` | `[]Message` | `messages` | `` |  |
| `TurnCount` | `int` | `turn_count` | `` |  |
| `Variables` | `map[string]any` | `variables,omitempty` | `` |  |
| `CreatedAt` | `time.Time` | `created_at` | `` |  |
| `LastAccess` | `time.Time` | `last_access` | `` |  |
| `TTL` | `time.Duration` | `-` | `` |  |
| `MaxHistory` | `int` | `-` | `` | MaxHistory caps len(Messages); older entries are dropped after each completed turn (audit H-06). 0 = unlimited. |

## internal/engine.ToolCallResult

Source: [internal/engine/tool_processor.go](../../../internal/engine/tool_processor.go#L20).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `ID` | `string` | `id` | `` |  |
| `ToolName` | `string` | `tool_name` | `` |  |
| `Response` | `any` | `response,omitempty` | `` |  |
| `Error` | `*types.ToolError` | `error,omitempty` | `` |  |
| `IsError` | `bool` | `is_error` | `` |  |

## internal/mcp.Request

Source: [internal/mcp/jsonrpc.go](../../../internal/mcp/jsonrpc.go#L30).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `JSONRPC` | `string` | `jsonrpc` | `` |  |
| `ID` | `json.RawMessage` | `id,omitempty` | `` |  |
| `Method` | `string` | `method` | `` |  |
| `Params` | `json.RawMessage` | `params,omitempty` | `` |  |
| `Result` | `json.RawMessage` | `result,omitempty` | `` | Result / RawError mark a decoded message that is actually a client RESPONSE to a server-initiated request, not a request — transports may legally deliver those (Streamable HTTP: ack 202, no body — round-10 R10-7). |
| `RawError` | `json.RawMessage` | `error,omitempty` | `` |  |

## internal/mcp.Response

Source: [internal/mcp/jsonrpc.go](../../../internal/mcp/jsonrpc.go#L56).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `JSONRPC` | `string` | `jsonrpc` | `` |  |
| `ID` | `json.RawMessage` | `id,omitempty` | `` |  |
| `Result` | `any` | `result,omitempty` | `` |  |
| `Error` | `*RPCError` | `error,omitempty` | `` |  |

## internal/mcp.RPCError

Source: [internal/mcp/jsonrpc.go](../../../internal/mcp/jsonrpc.go#L64).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Code` | `int` | `code` | `` |  |
| `Message` | `string` | `message` | `` |  |
| `Data` | `any` | `data,omitempty` | `` |  |

## internal/mcp.Notification

Source: [internal/mcp/server.go](../../../internal/mcp/server.go#L50).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Method` | `string` | `method` | `` |  |
| `Params` | `map[string]any` | `params,omitempty` | `` |  |

## internal/mcp.initializeResult

Source: [internal/mcp/server.go](../../../internal/mcp/server.go#L258).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `ProtocolVersion` | `string` | `protocolVersion` | `` |  |
| `ServerInfo` | `map[string]string` | `serverInfo` | `` |  |
| `Capabilities` | `map[string]interface{}` | `capabilities` | `` |  |

## internal/mcp.toolListEntry

Source: [internal/mcp/server.go](../../../internal/mcp/server.go#L312).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Name` | `string` | `name` | `` |  |
| `Description` | `string` | `description,omitempty` | `` |  |
| `InputSchema` | `types.JSONSchemaObject` | `inputSchema` | `` | inputSchema is REQUIRED by the spec's Tool type — one schema-less tool previously made the ENTIRE tools/list unparseable for strict SDK clients (round-10 R10-5). Defaulted to {"type":"object"} at emission. |

## internal/mcp.toolCallParams

Source: [internal/mcp/server.go](../../../internal/mcp/server.go#L380).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Name` | `string` | `name` | `` |  |
| `Arguments` | `map[string]any` | `arguments,omitempty` | `` |  |

## internal/mcp.toolCallResult

Source: [internal/mcp/server.go](../../../internal/mcp/server.go#L385).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Content` | `[]types.MCPContentBlock` | `content` | `` |  |
| `IsError` | `bool` | `isError,omitempty` | `` |  |

## internal/mcp.resourceListEntry

Source: [internal/mcp/server.go](../../../internal/mcp/server.go#L525).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `URI` | `string` | `uri` | `` |  |
| `Name` | `string` | `name,omitempty` | `` |  |
| `Description` | `string` | `description,omitempty` | `` |  |
| `MimeType` | `string` | `mimeType,omitempty` | `` |  |

## internal/mcp.resourcesReadParams

Source: [internal/mcp/server.go](../../../internal/mcp/server.go#L555).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `URI` | `string` | `uri` | `` |  |

## internal/mcp.resourceContents

Source: [internal/mcp/server.go](../../../internal/mcp/server.go#L559).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `URI` | `string` | `uri` | `` |  |
| `MimeType` | `string` | `mimeType,omitempty` | `` |  |
| `Text` | `string` | `text,omitempty` | `` |  |
| `Blob` | `string` | `blob,omitempty` | `` |  |

## internal/mcp.resourceSubscribeParams

Source: [internal/mcp/server.go](../../../internal/mcp/server.go#L592).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `URI` | `string` | `uri` | `` |  |

## internal/mcp.promptListEntry

Source: [internal/mcp/server.go](../../../internal/mcp/server.go#L650).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Name` | `string` | `name` | `` |  |
| `Description` | `string` | `description,omitempty` | `` |  |
| `Arguments` | `[]types.MCPPromptArg` | `arguments,omitempty` | `` |  |

## internal/mcp.promptsGetParams

Source: [internal/mcp/server.go](../../../internal/mcp/server.go#L671).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Name` | `string` | `name` | `` |  |
| `Arguments` | `map[string]string` | `arguments,omitempty` | `` |  |

## internal/mcp.completionRef

Source: [internal/mcp/server.go](../../../internal/mcp/server.go#L727).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Type` | `string` | `type` | `` |  |
| `Name` | `string` | `name` | `` |  |
| `URI` | `string` | `uri` | `` | URI is how spec-shaped ref/resource references address a resource (ResourceTemplateReference — round-10 R10-3); the catalog's RefName matches either spelling. |

## internal/mcp.completionArgument

Source: [internal/mcp/server.go](../../../internal/mcp/server.go#L736).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Name` | `string` | `name` | `` |  |
| `Value` | `string` | `value` | `` |  |

## internal/mcp.completionParams

Source: [internal/mcp/server.go](../../../internal/mcp/server.go#L741).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Ref` | `completionRef` | `ref` | `` |  |
| `Argument` | `completionArgument` | `argument` | `` |  |

## internal/mcp.completionValues

Source: [internal/mcp/server.go](../../../internal/mcp/server.go#L746).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Values` | `[]string` | `values` | `` |  |
| `Total` | `int` | `total,omitempty` | `` |  |
| `HasMore` | `bool` | `hasMore,omitempty` | `` |  |

## internal/mcp.setLevelParams

Source: [internal/mcp/server.go](../../../internal/mcp/server.go#L846).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Level` | `string` | `level` | `` |  |

## internal/pricing.Usage

Source: [internal/pricing/extract.go](../../../internal/pricing/extract.go#L11).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `PromptTokens` | `int` | `prompt_tokens` | `` |  |
| `CompletionTokens` | `int` | `completion_tokens` | `` |  |
| `Model` | `string` | `model,omitempty` | `` |  |

## internal/pricing.Price

Source: [internal/pricing/pricing.go](../../../internal/pricing/pricing.go#L29).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Model` | `string` | `model` | `model` |  |
| `PromptPer1KUSD` | `float64` | `prompt_per_1k_usd` | `prompt_per_1k_usd` |  |
| `CompletionPer1KUSD` | `float64` | `completion_per_1k_usd` | `completion_per_1k_usd` |  |

## internal/pricing.fileDoc

Source: [internal/pricing/pricing.go](../../../internal/pricing/pricing.go#L133).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Prices` | `[]Price` | `` | `prices` |  |
| `Fallback` | `*Price` | `` | `fallback,omitempty` | Fallback is optional; when omitted, the zero-cost default is preserved from NewDefaultTable. |

## internal/quota.Config

Source: [internal/quota/quota.go](../../../internal/quota/quota.go#L44).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `RatePerSec` | `float64` | `rate_per_sec` | `` | RatePerSec is the sustained request rate. 0 = unlimited. |
| `RateBurst` | `int` | `rate_burst` | `` | RateBurst is the token-bucket capacity (max burst). When <= 0 it derives from RatePerSec (at least 1), so a caller can set just the rate. |
| `MonthlySpendUSD` | `float64` | `monthly_spend_usd` | `` | MonthlySpendUSD is the spend cap for the current UTC month. 0 = unlimited. |

## internal/quota.Usage

Source: [internal/quota/quota.go](../../../internal/quota/quota.go#L55).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Month` | `string` | `month` | `` | "2006-01" (UTC) |
| `SpendUSD` | `float64` | `spend_usd` | `` | accrued spend this month (shared when backend is configured) |

## internal/realtime.ClientEvent

Source: [internal/realtime/session.go](../../../internal/realtime/session.go#L44).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Type` | `string` | `type` | `` |  |
| `EventID` | `string` | `event_id,omitempty` | `` |  |
| `Session` | `json.RawMessage` | `session,omitempty` | `` | session.update |
| `Item` | `json.RawMessage` | `item,omitempty` | `` | conversation.item.create |
| `Audio` | `string` | `audio,omitempty` | `` | input_audio_buffer.append (base64) |
| `Response` | `json.RawMessage` | `response,omitempty` | `` | response.create inline overrides |
| `ItemID` | `string` | `item_id,omitempty` | `` | conversation.item.retrieve / delete / truncate |
| `ContentIndex` | `int` | `content_index,omitempty` | `` | truncate |
| `AudioEndMs` | `int` | `audio_end_ms,omitempty` | `` | truncate |
| `PreviousItemID` | `string` | `previous_item_id,omitempty` | `` | conversation.item.create: insert after this item ("" = append at the end; "root" = insert at the beginning). |
| `ResponseID` | `string` | `response_id,omitempty` | `` | response.cancel: target a specific response ("" = the in-progress default-conversation response). |

## internal/realtime.sessionUpdate

Source: [internal/realtime/session.go](../../../internal/realtime/session.go#L100).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Type` | `string` | `type` | `` | "realtime" &#124; "transcription" |
| `Model` | `string` | `model` | `` |  |
| `Voice` | `string` | `voice` | `` | beta top-level alias |
| `Instructions` | `string` | `instructions` | `` |  |
| `Modalities` | `[]string` | `modalities` | `` | beta alias |
| `OutputModalities` | `[]string` | `output_modalities` | `` | GA |
| `InputAudioTranscription` | `json.RawMessage` | `input_audio_transcription` | `` |  |
| `Tools` | `json.RawMessage` | `tools` | `` |  |
| `ToolChoice` | `json.RawMessage` | `tool_choice` | `` |  |
| `MaxOutputTokens` | `json.RawMessage` | `max_output_tokens` | `` |  |
| `Tracing` | `json.RawMessage` | `tracing` | `` |  |
| `Truncation` | `json.RawMessage` | `truncation` | `` |  |
| `Prompt` | `json.RawMessage` | `prompt` | `` |  |
| `Include` | `json.RawMessage` | `include` | `` |  |
| `TurnDetection` | `json.RawMessage` | `turn_detection` | `` | beta top-level alias |
| `InputAudioFormat` | `string` | `input_audio_format` | `` | beta alias ("pcm16", "g711_ulaw", "g711_alaw") |
| `OutputAudioFormat` | `string` | `output_audio_format` | `` | beta alias |
| `Audio` | `*struct { Input *struct { Transcription json.RawMessage `json:"transcription"` TurnDetection json.RawMessage `json:"turn_detection"` Format json.RawMessage `json:"format"` NoiseReduction json.RawMessage `json:"noise_reduction"` } `json:"input"` Output *struct { Voice string `json:"voice"` Format json.RawMessage `json:"format"` Speed *float64 `json:"speed"` } `json:"output"` }` | `audio` | `` |  |

## internal/realtime.responseConfig

Source: [internal/realtime/session.go](../../../internal/realtime/session.go#L828).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Instructions` | `string` | `instructions` | `` |  |
| `OutputModalities` | `[]string` | `output_modalities` | `` |  |
| `Modalities` | `[]string` | `modalities` | `` | beta alias |
| `Metadata` | `json.RawMessage` | `metadata` | `` |  |
| `Conversation` | `string` | `conversation` | `` | "auto" (default) &#124; "none" |
| `MaxOutputTokens` | `json.RawMessage` | `max_output_tokens` | `` |  |
| `Input` | `json.RawMessage` | `input` | `` | custom context (nil = use the conversation) |
| `Audio` | `*struct { Output *struct { Voice string `json:"voice"` Format json.RawMessage `json:"format"` } `json:"output"` }` | `audio` | `` |  |

## internal/realtime.parsedItem

Source: [internal/realtime/session.go](../../../internal/realtime/session.go#L1885).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `ID` | `string` | `id` | `` | client-supplied item id ("" → the server mints one) |
| `Type` | `string` | `type` | `` |  |
| `Role` | `string` | `role` | `` |  |
| `CallID` | `string` | `call_id` | `` |  |
| `Output` | `string` | `output` | `` |  |
| `Name` | `string` | `name` | `` |  |
| `Arguments` | `string` | `arguments` | `` |  |
| `Content` | `json.RawMessage` | `content` | `` | kept raw so the ack can echo it verbatim |

## internal/realtime.vadConfig

Source: [internal/realtime/vad.go](../../../internal/realtime/vad.go#L24).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Type` | `string` | `type` | `` | "server_vad" &#124; "semantic_vad" |
| `Threshold` | `float64` | `threshold` | `` |  |
| `PrefixPaddingMs` | `int` | `prefix_padding_ms` | `` |  |
| `SilenceDurationMs` | `int` | `silence_duration_ms` | `` |  |
| `CreateResponse` | `*bool` | `create_response` | `` | nil → true |
| `InterruptResponse` | `*bool` | `interrupt_response` | `` | nil → true (Phase 2: cancels an in-flight paced response on speech start) |
| `IdleTimeoutMs` | `int` | `idle_timeout_ms` | `` | Phase 2: 0 = off |
| `Eagerness` | `string` | `eagerness` | `` | semantic_vad: low&#124;medium&#124;high&#124;auto |

## internal/recording.Interaction

Source: [internal/recording/cassette.go](../../../internal/recording/cassette.go#L34).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `RecordedAt` | `time.Time` | `recorded_at` | `` |  |
| `Hash` | `string` | `hash` | `` |  |
| `Method` | `string` | `method` | `` |  |
| `Path` | `string` | `path` | `` |  |
| `RequestHeaders` | `map[string]string` | `request_headers,omitempty` | `` |  |
| `RequestBody` | `json.RawMessage` | `request_body,omitempty` | `` |  |
| `ResponseStatus` | `int` | `response_status` | `` |  |
| `ResponseHeaders` | `map[string]string` | `response_headers,omitempty` | `` |  |
| `ResponseBody` | `json.RawMessage` | `response_body,omitempty` | `` |  |
| `RequestBodyEncoding` | `string` | `request_body_encoding,omitempty` | `` | RequestBodyEncoding / ResponseBodyEncoding describe how a body that is NOT valid JSON was wrapped for storage: "text" (JSON string) or "base64" (JSON string of base64). Empty means the body is verbatim JSON, so every cassette recorded before audit M-28 keeps its exact shape. |
| `ResponseBodyEncoding` | `string` | `response_body_encoding,omitempty` | `` |  |
| `Streaming` | `bool` | `streaming,omitempty` | `` | Streaming is true for captured Server-Sent Events responses. When set, ResponseBody is empty and StreamEvents holds the ordered chunks that were pushed to the client. |
| `StreamEvents` | `[]StreamEvent` | `stream_events,omitempty` | `` |  |

## internal/recording.StreamEvent

Source: [internal/recording/cassette.go](../../../internal/recording/cassette.go#L62).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `DelayMs` | `int64` | `delay_ms` | `` |  |
| `Data` | `string` | `data` | `` |  |

## internal/recording.missResponse

Source: [internal/recording/diagnostics.go](../../../internal/recording/diagnostics.go#L16).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Error` | `string` | `error` | `` |  |
| `Method` | `string` | `method` | `` |  |
| `Path` | `string` | `path` | `` |  |
| `Hash` | `string` | `hash` | `` |  |
| `Nearest` | `*nearestMatch` | `nearest,omitempty` | `` |  |

## internal/recording.nearestMatch

Source: [internal/recording/diagnostics.go](../../../internal/recording/diagnostics.go#L24).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Hash` | `string` | `hash` | `` |  |
| `Method` | `string` | `method` | `` |  |
| `Path` | `string` | `path` | `` |  |
| `Similarity` | `float64` | `similarity` | `` |  |
| `Diff` | `[]diffEntry` | `diff` | `` |  |

## internal/recording.diffEntry

Source: [internal/recording/diagnostics.go](../../../internal/recording/diagnostics.go#L35).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Field` | `string` | `field` | `` |  |
| `Kind` | `string` | `kind` | `` |  |
| `CassetteValue` | `json.RawMessage` | `cassette_value` | `` |  |
| `RequestValue` | `json.RawMessage` | `request_value` | `` |  |

## internal/recording.vcrCassette

Source: [internal/recording/import_vcr.go](../../../internal/recording/import_vcr.go#L72).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Interactions` | `[]vcrInteraction` | `` | `interactions` |  |

## internal/recording.vcrInteraction

Source: [internal/recording/import_vcr.go](../../../internal/recording/import_vcr.go#L76).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Request` | `vcrRequest` | `` | `request` |  |
| `Response` | `vcrResponse` | `` | `response` |  |

## internal/recording.vcrRequest

Source: [internal/recording/import_vcr.go](../../../internal/recording/import_vcr.go#L81).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Method` | `string` | `` | `method` |  |
| `URI` | `string` | `` | `uri` |  |
| `Body` | `vcrBody` | `` | `body` |  |
| `Headers` | `map[string][]string` | `` | `headers` |  |

## internal/recording.vcrResponse

Source: [internal/recording/import_vcr.go](../../../internal/recording/import_vcr.go#L88).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Status` | `vcrStatus` | `` | `status` |  |
| `Headers` | `map[string][]string` | `` | `headers` |  |
| `Body` | `vcrBody` | `` | `body` |  |

## internal/recording.vcrStatus

Source: [internal/recording/import_vcr.go](../../../internal/recording/import_vcr.go#L94).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Code` | `int` | `` | `code` |  |
| `Message` | `string` | `` | `message` |  |

## internal/runner.CaseResult

Source: [internal/runner/runner.go](../../../internal/runner/runner.go#L47).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Name` | `string` | `name` | `` |  |
| `Passed` | `bool` | `passed` | `` |  |
| `Failures` | `[]string` | `failures,omitempty` | `` |  |
| `Latency` | `time.Duration` | `latency` | `` |  |
| `FinalNode` | `string` | `final_node,omitempty` | `` |  |
| `ErrMessage` | `string` | `error,omitempty` | `` |  |

## internal/runner.SuiteResult

Source: [internal/runner/runner.go](../../../internal/runner/runner.go#L57).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `SuiteName` | `string` | `suite_name` | `` |  |
| `Target` | `string` | `target` | `` |  |
| `Cases` | `[]*CaseResult` | `cases` | `` |  |
| `Passed` | `int` | `passed` | `` |  |
| `Failed` | `int` | `failed` | `` |  |
| `Latency` | `time.Duration` | `latency` | `` |  |

## internal/server.AgentWriteResponse

Source: [internal/server/agent_write_handlers.go](../../../internal/server/agent_write_handlers.go#L198).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Status` | `string` | `status` | `` |  |
| `Agent` | `string` | `agent` | `` |  |
| `Persisted` | `bool` | `persisted` | `` |  |
| `File` | `string` | `file,omitempty` | `` |  |
| `Revision` | `string` | `revision,omitempty` | `` | Revision is the ETag of the agent AFTER this write, so a client can continue editing conditionally without re-fetching. Empty if it could not be computed — an unknown revision, never a stale one. |

## internal/server.CostGroup

Source: [internal/server/costs_handler.go](../../../internal/server/costs_handler.go#L22).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Key` | `string` | `key` | `` |  |
| `Requests` | `int` | `requests` | `` |  |
| `PromptTokens` | `int` | `prompt_tokens` | `` |  |
| `CompletionTokens` | `int` | `completion_tokens` | `` |  |
| `CostUSD` | `float64` | `cost_usd` | `` |  |

## internal/server.CostsResponse

Source: [internal/server/costs_handler.go](../../../internal/server/costs_handler.go#L31).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Window` | `struct { Since string `json:"since,omitempty"` Until string `json:"until,omitempty"` }` | `window` | `` |  |
| `Requests` | `int` | `total_requests` | `` |  |
| `PromptTokens` | `int` | `total_prompt_tokens` | `` |  |
| `CompletionTokens` | `int` | `total_completion_tokens` | `` |  |
| `CostUSD` | `float64` | `total_cost_usd` | `` |  |
| `ByModel` | `[]CostGroup` | `by_model` | `` |  |
| `ByAgent` | `[]CostGroup` | `by_agent` | `` |  |

## internal/server.HealthResponse

Source: [internal/server/handlers.go](../../../internal/server/handlers.go#L91).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Status` | `string` | `status` | `` |  |
| `Version` | `string` | `version` | `` |  |
| `Uptime` | `string` | `uptime` | `` | Uptime is the human-readable form (time.Duration.String); UptimeSeconds is the machine-readable one. Both are kept because the GUI reads the first and the published contract promised the second. |
| `UptimeSeconds` | `int64` | `uptime_seconds` | `` |  |
| `AgentsLoaded` | `int` | `agents_loaded` | `` |  |

## internal/server.ReloadResponse

Source: [internal/server/handlers.go](../../../internal/server/handlers.go#L107).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Status` | `string` | `status` | `` |  |
| `Agent` | `string` | `agent` | `` |  |

## internal/server.AgentSummary

Source: [internal/server/handlers.go](../../../internal/server/handlers.go#L113).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Name` | `string` | `name` | `` |  |
| `Description` | `string` | `description,omitempty` | `` |  |
| `Model` | `string` | `model` | `` |  |
| `Protocol` | `string` | `protocol` | `` |  |
| `ScenarioCount` | `int` | `scenario_count` | `` |  |
| `ToolCount` | `int` | `tool_count` | `` |  |
| `Tags` | `[]string` | `tags,omitempty` | `` |  |
| `EffectiveRevision` | `string` | `effective_revision,omitempty` | `` | EffectiveRevision is the revision of the RUNNING definition — the same value GET /agents/{name} publishes as X-Mockagents-Revision-Effective. Listing it lets a caller see at a glance which definitions have moved, without a request per agent. Empty when it could not be computed, which is not the same as unchanged. Deliberately NOT the ETag. The ETag combines this with a hash of the backing file, so computing it here would mean reading every agent's file on every listing — and a caller that mistook one for the other would send a precondition that always fails. Fetch the agent for its ETag. |
| `Persistence` | `string` | `persistence` | `` | Persistence answers "does this survive a restart", which a count of agents cannot. One of: file — backed by a file that is present now runtime — no backing file; created at runtime and lost on restart missing — a backing file is tracked but is not there any more, so the definition is serving from memory only "missing" is deliberately distinct from "runtime": one is a choice, the other is a surprise waiting for the next restart. |
| `File` | `string` | `file,omitempty` | `` | File is the base name of the backing file, when there is one. Only the base name: the absolute path is server-side detail a client has no use for and should not be handed. |

## internal/server.ErrorResponse

Source: [internal/server/handlers.go](../../../internal/server/handlers.go#L426).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Error` | `string` | `error` | `` |  |
| `AvailableAgents` | `[]string` | `available_agents,omitempty` | `` | AvailableAgents lists what the caller COULD have asked for, on a 404 for an unknown agent. A tenant-scoped list: it never names another tenant's. |
| `Details` | `string` | `details,omitempty` | `` | Details carries validator output when a write or reload failed schema validation. |

## internal/server.IdentityServer

Source: [internal/server/identity_handlers.go](../../../internal/server/identity_handlers.go#L35).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Version` | `string` | `version` | `` |  |

## internal/server.IdentityResponse

Source: [internal/server/identity_handlers.go](../../../internal/server/identity_handlers.go#L44).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Mode` | `string` | `mode` | `` |  |
| `Authenticated` | `bool` | `authenticated` | `` |  |
| `TenantID` | `string` | `tenant_id,omitempty` | `` |  |
| `KeyID` | `string` | `key_id,omitempty` | `` |  |
| `Role` | `*string` | `role` | `` |  |
| `Capabilities` | `[]string` | `capabilities` | `` |  |
| `Server` | `IdentityServer` | `server` | `` |  |

## internal/server.BroadcasterSnapshot

Source: [internal/server/log_broadcaster.go](../../../internal/server/log_broadcaster.go#L178).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `SubscriberCount` | `int` | `subscriber_count` | `` |  |
| `TotalDropped` | `uint64` | `total_dropped` | `` |  |
| `MaxDropped` | `uint64` | `max_dropped` | `` |  |
| `Subscribers` | `[]SubscriberSnapshot` | `subscribers` | `` |  |

## internal/server.SubscriberSnapshot

Source: [internal/server/log_broadcaster.go](../../../internal/server/log_broadcaster.go#L191).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Dropped` | `uint64` | `dropped` | `` |  |
| `BufferCap` | `int` | `buffer_cap` | `` |  |
| `BufferLen` | `int` | `buffer_len` | `` |  |

## internal/server.LogWithCost

Source: [internal/server/log_handlers.go](../../../internal/server/log_handlers.go#L38).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `(embedded)` | `storage.InteractionLog` | `` | `` |  |
| `PromptTokens` | `int` | `prompt_tokens` | `` |  |
| `CompletionTokens` | `int` | `completion_tokens` | `` |  |
| `Model` | `string` | `model,omitempty` | `` |  |
| `CostUSD` | `float64` | `cost_usd` | `` |  |

## internal/server.PipelineRunRequest

Source: [internal/server/pipeline_handlers.go](../../../internal/server/pipeline_handlers.go#L34).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Input` | `string` | `input` | `` |  |
| `SessionID` | `string` | `session_id,omitempty` | `` |  |

## internal/server.pipelineRunError

Source: [internal/server/pipeline_handlers.go](../../../internal/server/pipeline_handlers.go#L42).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Error` | `string` | `error` | `` |  |
| `Code` | `string` | `code,omitempty` | `` | Code classifies the failure so a caller can react without parsing Error. A pipeline that references an agent nobody loaded needs a different response from a scenario that failed to match — the first is fixed by loading a definition, the second by editing one — and telling them apart by substring is the kind of coupling that breaks on a reworded message. |
| `Result` | `*engine.PipelineResult` | `result,omitempty` | `` |  |

## internal/server.PipelineSummary

Source: [internal/server/pipeline_handlers.go](../../../internal/server/pipeline_handlers.go#L181).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Name` | `string` | `name` | `` |  |
| `Description` | `string` | `description,omitempty` | `` |  |
| `Topology` | `string` | `topology` | `` |  |
| `AgentCount` | `int` | `agent_count` | `` |  |
| `EdgeCount` | `int` | `edge_count` | `` |  |

## internal/server.pipelineNodeInput

Source: [internal/server/pipeline_recorder.go](../../../internal/server/pipeline_recorder.go#L47).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Pipeline` | `string` | `pipeline` | `` |  |
| `Node` | `string` | `node` | `` |  |
| `Input` | `string` | `input` | `` |  |

## internal/server.ProviderQuotaError

Source: [internal/server/quota_middleware.go](../../../internal/server/quota_middleware.go#L111).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Error` | `providerError` | `error` | `` |  |

## internal/server.providerError

Source: [internal/server/quota_middleware.go](../../../internal/server/quota_middleware.go#L115).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Type` | `string` | `type` | `` |  |
| `Message` | `string` | `message` | `` |  |

## internal/server.ReadinessCheckResult

Source: [internal/server/readiness_handlers.go](../../../internal/server/readiness_handlers.go#L39).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Name` | `string` | `name` | `` |  |
| `Status` | `string` | `status` | `` |  |
| `Error` | `string` | `error,omitempty` | `` |  |

## internal/server.ReadinessResponse

Source: [internal/server/readiness_handlers.go](../../../internal/server/readiness_handlers.go#L45).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Status` | `string` | `status` | `` |  |
| `Checks` | `[]ReadinessCheckResult` | `checks` | `` |  |

## internal/server.CreateTenantRequest

Source: [internal/server/tenancy_handlers.go](../../../internal/server/tenancy_handlers.go#L132).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Name` | `string` | `name` | `` |  |

## internal/server.CreateAPIKeyRequest

Source: [internal/server/tenancy_handlers.go](../../../internal/server/tenancy_handlers.go#L193).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Name` | `string` | `name` | `` |  |
| `Role` | `tenancy.Role` | `role` | `` |  |

## internal/server.UpdateAPIKeyRoleRequest

Source: [internal/server/tenancy_handlers.go](../../../internal/server/tenancy_handlers.go#L236).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Role` | `tenancy.Role` | `role` | `` |  |

## internal/server.BulkRotateResult

Source: [internal/server/tenancy_handlers.go](../../../internal/server/tenancy_handlers.go#L335).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Count` | `int` | `count` | `` |  |
| `Results` | `[]*tenancy.NewAPIKeyResult` | `results` | `` |  |

## internal/server.ValidateResponse

Source: [internal/server/validate_handler.go](../../../internal/server/validate_handler.go#L36).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `OK` | `bool` | `ok` | `` |  |
| `Kind` | `string` | `kind` | `` |  |
| `Errors` | `[]*config.ValidationError` | `errors` | `` |  |

## internal/storage.InteractionLog

Source: [internal/storage/models.go](../../../internal/storage/models.go#L4).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `ID` | `int64` | `id` | `` |  |
| `Timestamp` | `string` | `timestamp` | `` |  |
| `TenantID` | `string` | `tenant_id,omitempty` | `` |  |
| `AgentName` | `string` | `agent_name` | `` |  |
| `SessionID` | `string` | `session_id` | `` |  |
| `Protocol` | `string` | `protocol` | `` |  |
| `RequestMethod` | `string` | `request_method` | `` |  |
| `RequestPath` | `string` | `request_path` | `` |  |
| `RequestBody` | `string` | `request_body,omitempty` | `` |  |
| `ResponseStatus` | `int` | `response_status` | `` |  |
| `ResponseBody` | `string` | `response_body,omitempty` | `` |  |
| `LatencyMs` | `int64` | `latency_ms` | `` |  |
| `ToolCallsCount` | `int` | `tool_calls_count` | `` |  |
| `Streaming` | `bool` | `streaming` | `` |  |
| `Error` | `string` | `error,omitempty` | `` |  |
| `ScenarioName` | `string` | `scenario_name,omitempty` | `` |  |
| `ChaosAction` | `string` | `chaos_action,omitempty` | `` |  |
| `ChaosSource` | `string` | `chaos_source,omitempty` | `` |  |
| `ChaosSeed` | `*int64` | `chaos_seed,omitempty` | `` |  |
| `ChaosRate` | `*float64` | `chaos_rate,omitempty` | `` |  |
| `Truncated` | `bool` | `truncated,omitempty` | `` | Truncated reports that the request and/or response body exceeded the capture cap and the stored body is clipped, so a consumer knows the persisted body is not the complete payload. |
| `Source` | `string` | `source,omitempty` | `` | Source says what produced this interaction. Not every row is an HTTP request any more: a pipeline run drives the engine in process, and the results are real interactions that ought to be visible — but a reader who assumes an HTTP row would draw the wrong conclusions from an empty method, path and status. One of SourceHTTP or SourcePipeline. Rows written before this column existed read back as SourceHTTP, which is what they were. |

## internal/streaming.anthropicMessageStart

Source: [internal/streaming/anthropic.go](../../../internal/streaming/anthropic.go#L18).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Type` | `string` | `type` | `` |  |
| `Message` | `anthropicMessageHeader` | `message` | `` |  |

## internal/streaming.anthropicMessageHeader

Source: [internal/streaming/anthropic.go](../../../internal/streaming/anthropic.go#L23).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `ID` | `string` | `id` | `` |  |
| `Type` | `string` | `type` | `` |  |
| `Role` | `string` | `role` | `` |  |
| `Content` | `[]any` | `content` | `` |  |
| `Model` | `string` | `model` | `` |  |
| `StopReason` | `*string` | `stop_reason` | `` |  |
| `Usage` | `anthropicUsage` | `usage` | `` |  |

## internal/streaming.anthropicUsage

Source: [internal/streaming/anthropic.go](../../../internal/streaming/anthropic.go#L33).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `InputTokens` | `int` | `input_tokens` | `` |  |
| `OutputTokens` | `int` | `output_tokens` | `` |  |

## internal/streaming.anthropicContentBlockStart

Source: [internal/streaming/anthropic.go](../../../internal/streaming/anthropic.go#L38).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Type` | `string` | `type` | `` |  |
| `Index` | `int` | `index` | `` |  |
| `ContentBlock` | `any` | `content_block` | `` |  |

## internal/streaming.anthropicTextBlock

Source: [internal/streaming/anthropic.go](../../../internal/streaming/anthropic.go#L44).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Type` | `string` | `type` | `` |  |
| `Text` | `string` | `text` | `` |  |

## internal/streaming.anthropicToolUseBlock

Source: [internal/streaming/anthropic.go](../../../internal/streaming/anthropic.go#L49).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Type` | `string` | `type` | `` |  |
| `ID` | `string` | `id` | `` |  |
| `Name` | `string` | `name` | `` |  |
| `Input` | `map[string]any` | `input` | `` |  |

## internal/streaming.anthropicContentBlockDelta

Source: [internal/streaming/anthropic.go](../../../internal/streaming/anthropic.go#L56).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Type` | `string` | `type` | `` |  |
| `Index` | `int` | `index` | `` |  |
| `Delta` | `any` | `delta` | `` |  |

## internal/streaming.anthropicTextDelta

Source: [internal/streaming/anthropic.go](../../../internal/streaming/anthropic.go#L62).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Type` | `string` | `type` | `` |  |
| `Text` | `string` | `text` | `` |  |

## internal/streaming.anthropicInputJSONDelta

Source: [internal/streaming/anthropic.go](../../../internal/streaming/anthropic.go#L67).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Type` | `string` | `type` | `` |  |
| `PartialJSON` | `string` | `partial_json` | `` |  |

## internal/streaming.anthropicContentBlockStop

Source: [internal/streaming/anthropic.go](../../../internal/streaming/anthropic.go#L72).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Type` | `string` | `type` | `` |  |
| `Index` | `int` | `index` | `` |  |

## internal/streaming.anthropicMessageDelta

Source: [internal/streaming/anthropic.go](../../../internal/streaming/anthropic.go#L77).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Type` | `string` | `type` | `` |  |
| `Delta` | `struct { StopReason string `json:"stop_reason"` }` | `delta` | `` |  |
| `Usage` | `anthropicDeltaUsage` | `usage` | `` | Delta usage carries ONLY output_tokens: an input_tokens:0 here clobbers the message_start value in SDK accumulation (round-9 R9-10). |

## internal/streaming.anthropicDeltaUsage

Source: [internal/streaming/anthropic.go](../../../internal/streaming/anthropic.go#L88).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `OutputTokens` | `int` | `output_tokens` | `` |  |

## internal/streaming.anthropicMessageStop

Source: [internal/streaming/anthropic.go](../../../internal/streaming/anthropic.go#L92).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Type` | `string` | `type` | `` |  |

## internal/streaming.geminiStreamResponse

Source: [internal/streaming/gemini.go](../../../internal/streaming/gemini.go#L17).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Candidates` | `[]geminiStreamCandidate` | `candidates` | `` |  |
| `UsageMetadata` | `*geminiStreamUsage` | `usageMetadata,omitempty` | `` |  |

## internal/streaming.geminiStreamUsage

Source: [internal/streaming/gemini.go](../../../internal/streaming/gemini.go#L22).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `PromptTokenCount` | `int` | `promptTokenCount` | `` |  |
| `CandidatesTokenCount` | `int` | `candidatesTokenCount` | `` |  |
| `TotalTokenCount` | `int` | `totalTokenCount` | `` |  |

## internal/streaming.geminiStreamCandidate

Source: [internal/streaming/gemini.go](../../../internal/streaming/gemini.go#L28).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Content` | `geminiStreamContent` | `content` | `` |  |
| `FinishReason` | `string` | `finishReason,omitempty` | `` |  |
| `Index` | `int` | `index` | `` |  |

## internal/streaming.geminiStreamContent

Source: [internal/streaming/gemini.go](../../../internal/streaming/gemini.go#L34).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Role` | `string` | `role` | `` |  |
| `Parts` | `[]geminiStreamPart` | `parts` | `` |  |

## internal/streaming.geminiStreamPart

Source: [internal/streaming/gemini.go](../../../internal/streaming/gemini.go#L39).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Text` | `string` | `text,omitempty` | `` |  |
| `FunctionCall` | `*geminiStreamFunctionCall` | `functionCall,omitempty` | `` |  |

## internal/streaming.geminiStreamFunctionCall

Source: [internal/streaming/gemini.go](../../../internal/streaming/gemini.go#L44).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Name` | `string` | `name` | `` |  |
| `Args` | `map[string]any` | `args,omitzero` | `` | no-arg calls render "args": {} (R9-6) |

## internal/streaming.ChatCompletionChunk

Source: [internal/streaming/openai.go](../../../internal/streaming/openai.go#L17).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `ID` | `string` | `id` | `` |  |
| `Object` | `string` | `object` | `` |  |
| `Created` | `int64` | `created` | `` |  |
| `Model` | `string` | `model` | `` |  |
| `Choices` | `[]ChunkChoice` | `choices` | `` |  |
| `Usage` | `*ChunkUsage` | `usage,omitempty` | `` | Usage rides only the final empty-choices chunk when the request set stream_options {include_usage:true} (round-9 R9-9). |

## internal/streaming.ChunkUsage

Source: [internal/streaming/openai.go](../../../internal/streaming/openai.go#L29).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `PromptTokens` | `int` | `prompt_tokens` | `` |  |
| `CompletionTokens` | `int` | `completion_tokens` | `` |  |
| `TotalTokens` | `int` | `total_tokens` | `` |  |

## internal/streaming.ChunkChoice

Source: [internal/streaming/openai.go](../../../internal/streaming/openai.go#L36).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Index` | `int` | `index` | `` |  |
| `Delta` | `ChunkDelta` | `delta` | `` |  |
| `FinishReason` | `*string` | `finish_reason` | `` |  |

## internal/streaming.ChunkDelta

Source: [internal/streaming/openai.go](../../../internal/streaming/openai.go#L43).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Role` | `string` | `role,omitempty` | `` |  |
| `Content` | `string` | `content,omitempty` | `` |  |
| `ToolCalls` | `[]ChunkToolCall` | `tool_calls,omitempty` | `` |  |
| `Refusal` | `*string` | `refusal,omitempty` | `` | Refusal mirrors the non-streaming message.refusal field (FB-03), emitted when a scenario plants a refusal instead of content. |

## internal/streaming.ChunkToolCall

Source: [internal/streaming/openai.go](../../../internal/streaming/openai.go#L53).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Index` | `int` | `index` | `` |  |
| `ID` | `string` | `id,omitempty` | `` |  |
| `Type` | `string` | `type,omitempty` | `` |  |
| `Function` | `ChunkFunction` | `function` | `` |  |

## internal/streaming.ChunkFunction

Source: [internal/streaming/openai.go](../../../internal/streaming/openai.go#L64).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Name` | `string` | `name,omitempty` | `` |  |
| `Arguments` | `*string` | `arguments,omitempty` | `` |  |

## internal/tenancy.Tenant

Source: [internal/tenancy/types.go](../../../internal/tenancy/types.go#L75).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `ID` | `string` | `id` | `` |  |
| `Name` | `string` | `name` | `` |  |
| `CreatedAt` | `time.Time` | `created_at` | `` |  |

## internal/tenancy.APIKey

Source: [internal/tenancy/types.go](../../../internal/tenancy/types.go#L84).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `ID` | `string` | `id` | `` |  |
| `TenantID` | `string` | `tenant_id` | `` |  |
| `Name` | `string` | `name` | `` |  |
| `Prefix` | `string` | `prefix` | `` |  |
| `Role` | `Role` | `role` | `` |  |
| `CreatedAt` | `time.Time` | `created_at` | `` |  |
| `LastUsed` | `*time.Time` | `last_used,omitempty` | `` | LastUsed is a pointer so a never-used key omits the field entirely; `omitempty` is a no-op for a time.Time value (a struct is never the JSON empty value), which would otherwise emit "0001-01-01T00:00:00Z" (F-TY-001). |

## internal/tenancy.NewAPIKeyResult

Source: [internal/tenancy/types.go](../../../internal/tenancy/types.go#L101).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Key` | `APIKey` | `key` | `` |  |
| `Plaintext` | `string` | `plaintext` | `` |  |

## internal/tenancy.Principal

Source: [internal/tenancy/types.go](../../../internal/tenancy/types.go#L130).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `TenantID` | `string` | `-` | `` |  |
| `KeyID` | `string` | `-` | `` |  |
| `Role` | `Role` | `-` | `` |  |
| `CredentialVersion` | `int64` | `-` | `` | CredentialVersion changes whenever the key's authentication material changes. Cached bcrypt proofs are accepted only while this matches the authoritative row, which makes rotation/revocation safe across replicas. |

## internal/tenancy.User

Source: [internal/tenancy/types.go](../../../internal/tenancy/types.go#L197).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `ID` | `string` | `id` | `` |  |
| `Email` | `string` | `email` | `` |  |
| `TenantID` | `string` | `tenant_id` | `` |  |
| `Role` | `Role` | `role` | `` |  |
| `CreatedAt` | `time.Time` | `created_at` | `` |  |

## internal/tenancy.Session

Source: [internal/tenancy/types.go](../../../internal/tenancy/types.go#L208).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `UserID` | `string` | `user_id` | `` |  |
| `TenantID` | `string` | `tenant_id` | `` |  |
| `Role` | `Role` | `role` | `` |  |
| `CreatedAt` | `time.Time` | `created_at` | `` |  |
| `ExpiresAt` | `time.Time` | `expires_at` | `` |  |

## internal/types.A2AServerDefinition

Source: [internal/types/a2a.go](../../../internal/types/a2a.go#L21).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `APIVersion` | `string` | `apiVersion` | `apiVersion` |  |
| `Kind` | `string` | `kind` | `kind` |  |
| `Metadata` | `Metadata` | `metadata` | `metadata` |  |
| `Spec` | `A2AServerSpec` | `spec` | `spec` |  |

## internal/types.A2AServerSpec

Source: [internal/types/a2a.go](../../../internal/types/a2a.go#L29).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Card` | `A2AAgentCard` | `card` | `card` |  |
| `Responses` | `[]A2AMessageResponse` | `responses,omitempty` | `responses,omitempty` |  |
| `Faults` | `A2AFaults` | `faults,omitempty` | `faults,omitempty` |  |

## internal/types.A2AFaults

Source: [internal/types/a2a.go](../../../internal/types/a2a.go#L37).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Seed` | `int64` | `seed,omitempty` | `seed,omitempty` |  |
| `Rate` | `*float64` | `rate,omitempty` | `rate,omitempty` |  |
| `LatencyMs` | `int` | `latency_ms,omitempty` | `latency_ms,omitempty` |  |
| `TimeoutMs` | `int` | `timeout_ms,omitempty` | `timeout_ms,omitempty` |  |
| `StatusCode` | `int` | `status_code,omitempty` | `status_code,omitempty` |  |
| `Disconnect` | `bool` | `disconnect,omitempty` | `disconnect,omitempty` |  |
| `Reset` | `bool` | `reset,omitempty` | `reset,omitempty` |  |
| `Malformed` | `bool` | `malformed,omitempty` | `malformed,omitempty` |  |
| `MalformedSchema` | `bool` | `malformed_schema,omitempty` | `malformed_schema,omitempty` |  |
| `TruncateAfterBytes` | `int` | `truncate_after_bytes,omitempty` | `truncate_after_bytes,omitempty` |  |
| `Error` | `bool` | `error,omitempty` | `error,omitempty` |  |
| `OperationRates` | `map[string]float64` | `operation_rates,omitempty` | `operation_rates,omitempty` |  |
| `FixtureRates` | `map[string]float64` | `fixture_rates,omitempty` | `fixture_rates,omitempty` |  |
| `SequenceRates` | `map[uint64]float64` | `sequence_rates,omitempty` | `sequence_rates,omitempty` |  |

## internal/types.A2AAgentCard

Source: [internal/types/a2a.go](../../../internal/types/a2a.go#L66).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Name` | `string` | `name` | `name` |  |
| `Description` | `string` | `description,omitempty` | `description,omitempty` |  |
| `URL` | `string` | `url,omitempty` | `url,omitempty` |  |
| `Version` | `string` | `version,omitempty` | `version,omitempty` |  |
| `ProtocolVersion` | `string` | `protocolVersion` | `protocolVersion,omitempty` |  |
| `PreferredTransport` | `string` | `preferredTransport` | `preferredTransport,omitempty` | PreferredTransport names the transport served at `url`. It is optional in the v0.3 schema, but when set it MUST match the transport at `url`, and clients rely on it to choose how to call the agent. The server fills it with DefaultA2ATransport ("JSONRPC") at serve time so it always renders. |
| `DefaultInputModes` | `[]string` | `defaultInputModes,omitempty` | `defaultInputModes,omitempty` |  |
| `DefaultOutputModes` | `[]string` | `defaultOutputModes,omitempty` | `defaultOutputModes,omitempty` |  |
| `Capabilities` | `A2ACapabilities` | `capabilities` | `capabilities,omitempty` |  |
| `Skills` | `[]A2ASkill` | `skills` | `skills,omitempty` | Skills is a required array on the Agent Card; the server normalizes a nil slice to [] at serve time so it never renders as null/omitted. |

## internal/types.A2ACapabilities

Source: [internal/types/a2a.go](../../../internal/types/a2a.go#L86).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Streaming` | `bool` | `streaming` | `streaming,omitempty` |  |
| `PushNotifications` | `bool` | `pushNotifications` | `pushNotifications,omitempty` |  |

## internal/types.A2ASkill

Source: [internal/types/a2a.go](../../../internal/types/a2a.go#L92).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `ID` | `string` | `id` | `id` |  |
| `Name` | `string` | `name` | `name` |  |
| `Description` | `string` | `description,omitempty` | `description,omitempty` |  |
| `Tags` | `[]string` | `tags` | `tags,omitempty` | Tags is REQUIRED on every skill by the A2A spec; the server normalizes a nil slice to [] at serve time so the card always renders a JSON array (not null), which spec-strict clients require. |
| `Examples` | `[]string` | `examples,omitempty` | `examples,omitempty` |  |

## internal/types.A2AMessageResponse

Source: [internal/types/a2a.go](../../../internal/types/a2a.go#L106).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Match` | `string` | `match,omitempty` | `match,omitempty` |  |
| `Default` | `bool` | `default,omitempty` | `default,omitempty` |  |
| `Text` | `string` | `text,omitempty` | `text,omitempty` | Text is the agent's reply; it becomes the task's artifact and status message. |
| `State` | `string` | `state,omitempty` | `state,omitempty` | State is the terminal task state to report (default "completed"); set e.g. "failed" or "input-required" to exercise non-happy paths. |
| `AsMessage` | `bool` | `as_message,omitempty` | `as_message,omitempty` | AsMessage makes message/send return a bare Message result instead of a Task (the A2A result is Task&#124;Message — a quick, stateless reply needs no task). message/stream always yields a Task regardless. |
| `Data` | `any` | `data,omitempty` | `data,omitempty` | Data, when set, is emitted as a structured `data` Part on the reply (alongside the text Part), exercising non-text A2A parts. |

## internal/types.AgentDefinition

Source: [internal/types/agent.go](../../../internal/types/agent.go#L10).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `APIVersion` | `string` | `apiVersion` | `apiVersion` |  |
| `Kind` | `string` | `kind` | `kind` |  |
| `Metadata` | `Metadata` | `metadata` | `metadata` |  |
| `Spec` | `AgentSpec` | `spec` | `spec` |  |

## internal/types.Metadata

Source: [internal/types/agent.go](../../../internal/types/agent.go#L24).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Name` | `string` | `name` | `name` |  |
| `Description` | `string` | `description,omitempty` | `description,omitempty` |  |
| `Tags` | `[]string` | `tags,omitempty` | `tags,omitempty` |  |
| `TenantID` | `string` | `tenant_id,omitempty` | `tenant_id,omitempty` |  |

## internal/types.AgentSpec

Source: [internal/types/agent.go](../../../internal/types/agent.go#L32).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Protocol` | `string` | `protocol` | `protocol` |  |
| `Model` | `string` | `model,omitempty` | `model,omitempty` |  |
| `SystemPrompt` | `string` | `systemPrompt,omitempty` | `systemPrompt,omitempty` |  |
| `Tools` | `[]ToolDefinition` | `tools,omitempty` | `tools,omitempty` |  |
| `Behavior` | `BehaviorConfig` | `behavior` | `behavior` |  |

## internal/types.BehaviorConfig

Source: [internal/types/behavior.go](../../../internal/types/behavior.go#L6).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Scenarios` | `[]Scenario` | `scenarios` | `scenarios` |  |
| `Streaming` | `*StreamingConfig` | `streaming,omitempty` | `streaming,omitempty` |  |
| `Chaos` | `*ChaosConfig` | `chaos,omitempty` | `chaos,omitempty` |  |
| `StrictTools` | `*StrictToolsConfig` | `strict_tools,omitempty` | `strict_tools,omitempty` |  |

## internal/types.StrictToolsConfig

Source: [internal/types/behavior.go](../../../internal/types/behavior.go#L23).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Level` | `string` | `level,omitempty` | `level,omitempty` |  |
| `IDs` | `*bool` | `ids,omitempty` | `ids,omitempty` | IDs gates round-trip tool id validation: every tool result must reference a tool call echoed earlier in the request history. |
| `ToolChoice` | `*bool` | `tool_choice,omitempty` | `tool_choice,omitempty` | ToolChoice gates required/named tool_choice enforcement (incl. the parallel-call cap, which real APIs apply via the same parameter family). |
| `Schemas` | `*bool` | `schemas,omitempty` | `schemas,omitempty` | Schemas gates strict:true function-schema subset validation. |

## internal/types.Scenario

Source: [internal/types/behavior.go](../../../internal/types/behavior.go#L41).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Name` | `string` | `name` | `name` |  |
| `Match` | `*MatchRule` | `match,omitempty` | `match,omitempty` |  |
| `Response` | `ScenarioResponse` | `response` | `response` |  |

## internal/types.MatchRule

Source: [internal/types/behavior.go](../../../internal/types/behavior.go#L48).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `ContentContains` | `string` | `content_contains,omitempty` | `content_contains,omitempty` |  |
| `ContentRegex` | `string` | `content_regex,omitempty` | `content_regex,omitempty` |  |
| `TurnNumber` | `*int` | `turn_number,omitempty` | `turn_number,omitempty` |  |
| `HasImage` | `*bool` | `has_image,omitempty` | `has_image,omitempty` | HasImage, when set, matches only when the latest user turn carries at least one image content part (true) or none (false) — A-05 vision matching. The image signal is out-of-band (the flattened user text stays pure, so content_regex/templates are unaffected). |

## internal/types.ScenarioResponse

Source: [internal/types/behavior.go](../../../internal/types/behavior.go#L60).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Content` | `string` | `content` | `content` |  |
| `ToolCalls` | `[]ToolCallSpec` | `tool_calls,omitempty` | `tool_calls,omitempty` |  |
| `Metadata` | `map[string]any` | `metadata,omitempty` | `metadata,omitempty` |  |
| `FinishReason` | `string` | `finish_reason,omitempty` | `finish_reason,omitempty` | FinishReason overrides the emitted finish/stop reason (FB-03 semantic errors). Given as an OpenAI-style value ("length", "content_filter", "stop", "tool_calls") and mapped per provider (Anthropic stop_reason, Gemini finishReason). Use "length" to simulate a truncated response. |
| `Refusal` | `string` | `refusal,omitempty` | `refusal,omitempty` | Refusal, when set, emits an assistant refusal instead of normal content — OpenAI's structured `message.refusal` field (and the refusal text as content on Anthropic/Gemini), to exercise refusal-handling code paths. |
| `Hallucination` | `*HallucinationSpec` | `hallucination,omitempty` | `hallucination,omitempty` |  |

## internal/types.HallucinationSpec

Source: [internal/types/behavior.go](../../../internal/types/behavior.go#L83).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Type` | `string` | `type,omitempty` | `type,omitempty` | Type categorizes the planted fault. One of: fabricated_fact, fabricated_citation, ungrounded, bad_tool_result, other (default: other). |
| `GroundTruth` | `string` | `ground_truth,omitempty` | `ground_truth,omitempty` | GroundTruth is the correct answer — documentation for the test author and a reference an assertion can compare against. |
| `Note` | `string` | `note,omitempty` | `note,omitempty` | Note explains why the output is wrong. |

## internal/types.ToolCallSpec

Source: [internal/types/behavior.go](../../../internal/types/behavior.go#L98).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Name` | `string` | `name` | `name` |  |
| `Arguments` | `map[string]any` | `arguments,omitempty` | `arguments,omitempty` |  |
| `RawArguments` | `string` | `raw_arguments,omitempty` | `raw_arguments,omitempty` | RawArguments, when set, is emitted VERBATIM as the tool call's argument string instead of marshaling Arguments — so a scenario can plant malformed or schema-violating JSON (e.g. `{"city":`) to exercise a client's tool-call argument parser (FB-03 semantic errors). OpenAI only (the provider whose tool-call arguments are a JSON string). |

## internal/types.StreamingConfig

Source: [internal/types/behavior.go](../../../internal/types/behavior.go#L141).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Enabled` | `bool` | `enabled` | `enabled` |  |
| `ChunkSize` | `int` | `chunk_size,omitempty` | `chunk_size,omitempty` |  |
| `ChunkDelayMs` | `*int` | `chunk_delay_ms,omitempty` | `chunk_delay_ms,omitempty` | ChunkDelayMs is the delay between content chunks, in milliseconds. A POINTER because zero is a meaningful value here: `chunk_delay_ms: 0` means "stream with no artificial delay", which the JSON schema documents (`minimum: 0`) and which examples/gemini-agent.yaml asks for. As a plain int an explicit 0 was indistinguishable from unset, so ApplyDefaults overwrote it with 50 and the agent silently ran ~50ms slower per chunk than it said it would. nil means unset: ApplyDefaults fills it, and each streaming path falls back to DefaultChunkDelayMs for a config that never went through ApplyDefaults. ChunkSize keeps a plain int on purpose — a chunk size of zero is meaningless, so zero-as-unset is correct there. |
| `TTFTMs` | `int` | `ttft_ms,omitempty` | `ttft_ms,omitempty` | TTFTMs is the time-to-first-token: a delay before the first content chunk is emitted (the structural opening frame is sent immediately). |
| `TokensPerSec` | `float64` | `tokens_per_sec,omitempty` | `tokens_per_sec,omitempty` | TokensPerSec, when > 0, paces content chunks at this rate (the per-chunk delay is derived from the chunk length), overriding ChunkDelayMs. |
| `JitterMs` | `int` | `jitter_ms,omitempty` | `jitter_ms,omitempty` | JitterMs adds a deterministic +/- jitter (up to this many ms) to each inter-chunk delay, modeling network variance. |
| `TTFTP50Ms` | `int` | `ttft_p50_ms,omitempty` | `ttft_p50_ms,omitempty` | TTFTP50Ms / TTFTP95Ms are the median and 95th-percentile time-to-first-token. |
| `TTFTP95Ms` | `int` | `ttft_p95_ms,omitempty` | `ttft_p95_ms,omitempty` |  |
| `ITLP50Ms` | `int` | `itl_p50_ms,omitempty` | `itl_p50_ms,omitempty` | ITLP50Ms / ITLP95Ms are the median and 95th-percentile inter-token latency (per token); the per-chunk delay is the sample times the chunk's token count. |
| `ITLP95Ms` | `int` | `itl_p95_ms,omitempty` | `itl_p95_ms,omitempty` |  |
| `TruncateAfterChunks` | `int` | `truncate_after_chunks,omitempty` | `truncate_after_chunks,omitempty` | TruncateAfterChunks, when > 0, ends the stream after this many content chunks WITHOUT the terminating finish frame / [DONE] sentinel — a truncated stream, to test client robustness to early disconnects. |
| `Malformed` | `bool` | `malformed,omitempty` | `malformed,omitempty` | Malformed, when true, emits one deliberately invalid JSON SSE frame at the stop point and then ends the stream (no finish frame / [DONE]) — to test client parser/error handling of malformed chunks and tool calls. |

## internal/types.ChaosConfig

Source: [internal/types/behavior.go](../../../internal/types/behavior.go#L201).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Enabled` | `bool` | `enabled,omitempty` | `enabled,omitempty` |  |
| `Preset` | `string` | `preset,omitempty` | `preset,omitempty` | Preset is a named shorthand (e.g. "server-down", "rate-limited", "access-denied") that expands at load time into the concrete sub-sections below. Explicitly-set sub-sections take precedence over the preset's values. See ChaosPresets for the recognized names. |
| `Latency` | `*ChaosLatencyConfig` | `latency,omitempty` | `latency,omitempty` |  |
| `Errors` | `*ChaosErrorConfig` | `errors,omitempty` | `errors,omitempty` |  |
| `RateLimit` | `*ChaosRateLimitConfig` | `rate_limit,omitempty` | `rate_limit,omitempty` |  |
| `Connection` | `*ChaosConnectionConfig` | `connection,omitempty` | `connection,omitempty` |  |

## internal/types.ChaosLatencyConfig

Source: [internal/types/behavior.go](../../../internal/types/behavior.go#L222).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Distribution` | `string` | `distribution,omitempty` | `distribution,omitempty` |  |
| `MinMs` | `int` | `min_ms,omitempty` | `min_ms,omitempty` |  |
| `MaxMs` | `int` | `max_ms,omitempty` | `max_ms,omitempty` |  |
| `MeanMs` | `int` | `mean_ms,omitempty` | `mean_ms,omitempty` |  |
| `StddevMs` | `int` | `stddev_ms,omitempty` | `stddev_ms,omitempty` |  |

## internal/types.ChaosErrorConfig

Source: [internal/types/behavior.go](../../../internal/types/behavior.go#L237).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Rate` | `float64` | `rate,omitempty` | `rate,omitempty` |  |
| `StatusCode` | `int` | `status_code,omitempty` | `status_code,omitempty` |  |
| `StatusCodes` | `[]int` | `status_codes,omitempty` | `status_codes,omitempty` |  |
| `Timeout` | `bool` | `timeout,omitempty` | `timeout,omitempty` |  |
| `TimeoutMs` | `int` | `timeout_ms,omitempty` | `timeout_ms,omitempty` |  |
| `Message` | `string` | `message,omitempty` | `message,omitempty` |  |
| `FailFirst` | `int` | `fail_first,omitempty` | `fail_first,omitempty` | FailFirst, when > 0, deterministically injects the error on the first N requests to the agent and then RECOVERS (every request after the Nth succeeds) — a stateful "flaky then healthy" trigger for exercising client retry/backoff/circuit-breaker logic, which is otherwise impossible to test reproducibly against a live API. It takes precedence over Rate: the first N requests always fail regardless of the probability, then injection stops. The count is per agent and resets when the server restarts. |

## internal/types.ChaosRateLimitConfig

Source: [internal/types/behavior.go](../../../internal/types/behavior.go#L256).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Requests` | `int` | `requests` | `requests` |  |
| `WindowMs` | `int` | `window_ms` | `window_ms` |  |

## internal/types.ChaosConnectionConfig

Source: [internal/types/behavior.go](../../../internal/types/behavior.go#L278).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Mode` | `string` | `mode` | `mode` |  |
| `Rate` | `float64` | `rate,omitempty` | `rate,omitempty` |  |
| `FailFirst` | `int` | `fail_first,omitempty` | `fail_first,omitempty` |  |

## internal/types.MCPServerDefinition

Source: [internal/types/mcp.go](../../../internal/types/mcp.go#L16).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `APIVersion` | `string` | `apiVersion` | `apiVersion` |  |
| `Kind` | `string` | `kind` | `kind` |  |
| `Metadata` | `Metadata` | `metadata` | `metadata` |  |
| `Spec` | `MCPServerSpec` | `spec` | `spec` |  |

## internal/types.MCPServerSpec

Source: [internal/types/mcp.go](../../../internal/types/mcp.go#L25).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `ProtocolVersion` | `string` | `protocolVersion,omitempty` | `protocolVersion,omitempty` |  |
| `Capabilities` | `MCPCapabilities` | `capabilities,omitempty` | `capabilities,omitempty` |  |
| `Tools` | `[]MCPTool` | `tools,omitempty` | `tools,omitempty` |  |
| `Resources` | `[]MCPResource` | `resources,omitempty` | `resources,omitempty` |  |
| `Prompts` | `[]MCPPrompt` | `prompts,omitempty` | `prompts,omitempty` |  |
| `Completions` | `[]MCPCompletion` | `completions,omitempty` | `completions,omitempty` |  |
| `StrictArgs` | `*bool` | `strictArgs,omitempty` | `strictArgs,omitempty` | StrictArgs controls tools/call argument validation against each tool's inputSchema (round-11, closes R10-19). Default ON (nil = true): the MCP spec says servers MUST validate tool inputs, and official SDK servers do. Invalid argument VALUES are reported as an isError:true execution result per the 2025-11-25 revision — never a -32602 protocol error. Set strictArgs: false to restore the old accept-anything behavior. |
| `Faults` | `MCPFaults` | `faults,omitempty` | `faults,omitempty` |  |

## internal/types.MCPFaults

Source: [internal/types/mcp.go](../../../internal/types/mcp.go#L45).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Seed` | `int64` | `seed,omitempty` | `seed,omitempty` |  |
| `Rate` | `*float64` | `rate,omitempty` | `rate,omitempty` |  |
| `LatencyMs` | `int` | `latency_ms,omitempty` | `latency_ms,omitempty` |  |
| `TimeoutMs` | `int` | `timeout_ms,omitempty` | `timeout_ms,omitempty` |  |
| `StatusCode` | `int` | `status_code,omitempty` | `status_code,omitempty` |  |
| `Disconnect` | `bool` | `disconnect,omitempty` | `disconnect,omitempty` |  |
| `Reset` | `bool` | `reset,omitempty` | `reset,omitempty` |  |
| `Malformed` | `bool` | `malformed,omitempty` | `malformed,omitempty` |  |
| `MalformedSchema` | `bool` | `malformed_schema,omitempty` | `malformed_schema,omitempty` |  |
| `TruncateAfterBytes` | `int` | `truncate_after_bytes,omitempty` | `truncate_after_bytes,omitempty` |  |
| `Error` | `bool` | `error,omitempty` | `error,omitempty` |  |
| `OperationRates` | `map[string]float64` | `operation_rates,omitempty` | `operation_rates,omitempty` |  |
| `FixtureRates` | `map[string]float64` | `fixture_rates,omitempty` | `fixture_rates,omitempty` |  |
| `SequenceRates` | `map[uint64]float64` | `sequence_rates,omitempty` | `sequence_rates,omitempty` |  |

## internal/types.MCPCapabilities

Source: [internal/types/mcp.go](../../../internal/types/mcp.go#L64).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Tools` | `bool` | `tools,omitempty` | `tools,omitempty` |  |
| `Resources` | `bool` | `resources,omitempty` | `resources,omitempty` |  |
| `Prompts` | `bool` | `prompts,omitempty` | `prompts,omitempty` |  |
| `Logging` | `bool` | `logging,omitempty` | `logging,omitempty` |  |

## internal/types.MCPTool

Source: [internal/types/mcp.go](../../../internal/types/mcp.go#L72).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Name` | `string` | `name` | `name` |  |
| `Description` | `string` | `description,omitempty` | `description,omitempty` |  |
| `InputSchema` | `JSONSchemaObject` | `inputSchema,omitempty` | `inputSchema,omitempty` |  |
| `Responses` | `[]MCPToolResponse` | `responses,omitempty` | `responses,omitempty` |  |

## internal/types.MCPToolResponse

Source: [internal/types/mcp.go](../../../internal/types/mcp.go#L82).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Match` | `map[string]any` | `match,omitempty` | `match,omitempty` |  |
| `Default` | `bool` | `default,omitempty` | `default,omitempty` |  |
| `Content` | `[]MCPContentBlock` | `content,omitempty` | `content,omitempty` |  |
| `IsError` | `bool` | `isError,omitempty` | `isError,omitempty` |  |

## internal/types.MCPContentBlock

Source: [internal/types/mcp.go](../../../internal/types/mcp.go#L96).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Type` | `string` | `type` | `type` |  |
| `Text` | `string` | `text,omitempty` | `text,omitempty` |  |
| `Data` | `string` | `data,omitempty` | `data,omitempty` |  |
| `MimeType` | `string` | `mimeType,omitempty` | `mimeType,omitempty` |  |
| `URI` | `string` | `uri,omitempty` | `uri,omitempty` |  |
| `Blob` | `string` | `blob,omitempty` | `blob,omitempty` |  |

## internal/types.MCPResource

Source: [internal/types/mcp.go](../../../internal/types/mcp.go#L138).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `URI` | `string` | `uri` | `uri` |  |
| `Name` | `string` | `name,omitempty` | `name,omitempty` |  |
| `Description` | `string` | `description,omitempty` | `description,omitempty` |  |
| `MimeType` | `string` | `mimeType,omitempty` | `mimeType,omitempty` |  |
| `Text` | `string` | `text,omitempty` | `text,omitempty` |  |
| `Blob` | `string` | `blob,omitempty` | `blob,omitempty` |  |

## internal/types.MCPPrompt

Source: [internal/types/mcp.go](../../../internal/types/mcp.go#L148).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Name` | `string` | `name` | `name` |  |
| `Description` | `string` | `description,omitempty` | `description,omitempty` |  |
| `Arguments` | `[]MCPPromptArg` | `arguments,omitempty` | `arguments,omitempty` |  |
| `Messages` | `[]MCPPromptMessage` | `messages,omitempty` | `messages,omitempty` |  |

## internal/types.MCPPromptArg

Source: [internal/types/mcp.go](../../../internal/types/mcp.go#L156).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Name` | `string` | `name` | `name` |  |
| `Description` | `string` | `description,omitempty` | `description,omitempty` |  |
| `Required` | `bool` | `required,omitempty` | `required,omitempty` |  |

## internal/types.MCPPromptMessage

Source: [internal/types/mcp.go](../../../internal/types/mcp.go#L163).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Role` | `string` | `role` | `role` |  |
| `Content` | `MCPContentBlock` | `content` | `content` |  |

## internal/types.MCPCompletion

Source: [internal/types/mcp.go](../../../internal/types/mcp.go#L175).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `RefType` | `string` | `refType,omitempty` | `refType,omitempty` | RefType is "ref/prompt" or "ref/resource". Empty matches both. |
| `RefName` | `string` | `refName,omitempty` | `refName,omitempty` | RefName is the prompt or resource template name. Empty matches any. |
| `ArgName` | `string` | `argName` | `argName` | ArgName is the argument the suggestions belong to. |
| `Values` | `[]string` | `values` | `values` | Values is the static list of candidate completions. |

## internal/types.PipelineDefinition

Source: [internal/types/pipeline.go](../../../internal/types/pipeline.go#L12).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `APIVersion` | `string` | `apiVersion` | `apiVersion` |  |
| `Kind` | `string` | `kind` | `kind` |  |
| `Metadata` | `Metadata` | `metadata` | `metadata` |  |
| `Spec` | `PipelineSpec` | `spec` | `spec` |  |

## internal/types.PipelineSpec

Source: [internal/types/pipeline.go](../../../internal/types/pipeline.go#L20).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Topology` | `string` | `topology` | `topology` |  |
| `Agents` | `[]PipelineAgent` | `agents` | `agents` |  |
| `Edges` | `[]PipelineEdge` | `edges,omitempty` | `edges,omitempty` |  |

## internal/types.PipelineAgent

Source: [internal/types/pipeline.go](../../../internal/types/pipeline.go#L27).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `ID` | `string` | `id` | `id` |  |
| `Ref` | `string` | `ref` | `ref` |  |

## internal/types.PipelineEdge

Source: [internal/types/pipeline.go](../../../internal/types/pipeline.go#L41).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `From` | `string` | `from` | `from` |  |
| `To` | `string` | `to` | `to` |  |
| `WhenContains` | `string` | `when_contains,omitempty` | `when_contains,omitempty` |  |

## internal/types.SearchServiceDefinition

Source: [internal/types/search.go](../../../internal/types/search.go#L5).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `APIVersion` | `string` | `apiVersion` | `apiVersion` |  |
| `Kind` | `string` | `kind` | `kind` |  |
| `Metadata` | `Metadata` | `metadata` | `metadata` |  |
| `Spec` | `SearchServiceSpec` | `spec` | `spec` |  |

## internal/types.SearchServiceSpec

Source: [internal/types/search.go](../../../internal/types/search.go#L12).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Provider` | `string` | `provider` | `provider` |  |
| `Scenarios` | `[]SearchScenario` | `scenarios,omitempty` | `scenarios,omitempty` |  |
| `Faults` | `SearchFaults` | `faults,omitempty` | `faults,omitempty` |  |

## internal/types.SearchFaults

Source: [internal/types/search.go](../../../internal/types/search.go#L18).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Seed` | `int64` | `seed,omitempty` | `seed,omitempty` | Seed and Rate gate configured faults deterministically per request. A nil Rate preserves the legacy always-on behavior. X-Mockagents-Chaos can force a configured action or disable chaos for one request. |
| `Rate` | `*float64` | `rate,omitempty` | `rate,omitempty` |  |
| `OperationRates` | `map[string]float64` | `operation_rates,omitempty` | `operation_rates,omitempty` |  |
| `LatencyMs` | `int` | `latency_ms,omitempty` | `latency_ms,omitempty` |  |
| `StatusCode` | `int` | `status_code,omitempty` | `status_code,omitempty` |  |
| `MalformedJSON` | `bool` | `malformed_json,omitempty` | `malformed_json,omitempty` |  |
| `Disconnect` | `bool` | `disconnect,omitempty` | `disconnect,omitempty` |  |
| `PartialResults` | `*SearchPartialResultsFault` | `partial_results,omitempty` | `partial_results,omitempty` |  |
| `GlobalSeed` | `int64` | `-` | `-` | GlobalSeed and GlobalRate are runtime-only defaults supplied by `mockagents start`; declarative service fields remain the higher scope. |
| `GlobalRate` | `*float64` | `-` | `-` |  |

## internal/types.SearchPartialResultsFault

Source: [internal/types/search.go](../../../internal/types/search.go#L36).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `MaxResults` | `int` | `max_results` | `max_results` |  |

## internal/types.SearchScenario

Source: [internal/types/search.go](../../../internal/types/search.go#L40).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Name` | `string` | `name` | `name` |  |
| `Match` | `SearchMatch` | `match,omitempty` | `match,omitempty` |  |
| `Response` | `SearchResponse` | `response` | `response` |  |

## internal/types.SearchMatch

Source: [internal/types/search.go](../../../internal/types/search.go#L45).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `QueryContains` | `string` | `query_contains,omitempty` | `query_contains,omitempty` |  |
| `QueryRegex` | `string` | `query_regex,omitempty` | `query_regex,omitempty` |  |
| `Default` | `bool` | `default,omitempty` | `default,omitempty` |  |

## internal/types.SearchResponse

Source: [internal/types/search.go](../../../internal/types/search.go#L50).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Answer` | `string` | `answer,omitempty` | `answer,omitempty` |  |
| `Results` | `[]SearchResult` | `results,omitempty` | `results,omitempty` |  |

## internal/types.SearchResult

Source: [internal/types/search.go](../../../internal/types/search.go#L54).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Title` | `string` | `title` | `title` |  |
| `URL` | `string` | `url` | `url` |  |
| `Content` | `string` | `content` | `content` |  |
| `Score` | `float64` | `score` | `score` |  |
| `RawContent` | `*string` | `raw_content,omitempty` | `raw_content,omitempty` |  |
| `Favicon` | `string` | `favicon,omitempty` | `favicon,omitempty` |  |
| `PublishedDate` | `string` | `published_date,omitempty` | `published_date,omitempty` |  |

## internal/types.TestSuiteDefinition

Source: [internal/types/testsuite.go](../../../internal/types/testsuite.go#L42).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `APIVersion` | `string` | `apiVersion` | `apiVersion` |  |
| `Kind` | `string` | `kind` | `kind` |  |
| `Metadata` | `Metadata` | `metadata` | `metadata` |  |
| `Spec` | `TestSuiteSpec` | `spec` | `spec` |  |

## internal/types.TestSuiteSpec

Source: [internal/types/testsuite.go](../../../internal/types/testsuite.go#L50).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Target` | `TestTarget` | `target` | `target` |  |
| `Cases` | `[]TestCase` | `cases` | `cases` |  |

## internal/types.TestTarget

Source: [internal/types/testsuite.go](../../../internal/types/testsuite.go#L57).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Agent` | `string` | `agent,omitempty` | `agent,omitempty` |  |
| `Pipeline` | `string` | `pipeline,omitempty` | `pipeline,omitempty` |  |

## internal/types.TestCase

Source: [internal/types/testsuite.go](../../../internal/types/testsuite.go#L63).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Name` | `string` | `name` | `name` |  |
| `Steps` | `[]TestStep` | `steps` | `steps` |  |
| `Assertions` | `[]TestAssertion` | `assertions,omitempty` | `assertions,omitempty` |  |

## internal/types.TestStep

Source: [internal/types/testsuite.go](../../../internal/types/testsuite.go#L70).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Role` | `string` | `role` | `role` |  |
| `Content` | `string` | `content` | `content` |  |

## internal/types.TestAssertion

Source: [internal/types/testsuite.go](../../../internal/types/testsuite.go#L77).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Type` | `string` | `type` | `type` |  |
| `Tool` | `string` | `tool,omitempty` | `tool,omitempty` |  |
| `Args` | `map[string]any` | `arguments,omitempty` | `arguments,omitempty` |  |
| `Value` | `string` | `value,omitempty` | `value,omitempty` |  |
| `MaxMs` | `int64` | `max_ms,omitempty` | `max_ms,omitempty` |  |
| `Count` | `*int` | `count,omitempty` | `count,omitempty` | Count is the expected number of tool calls for tool_call_count (a pointer so an omitted count is distinguishable from an explicit 0 = "no tool calls"). |
| `Sequence` | `[]string` | `sequence,omitempty` | `sequence,omitempty` | Sequence is the expected ordered list for tool_call_sequence (tool-call names in the response) or node_sequence (pipeline node ids that ran). |
| `NodeID` | `string` | `node_id,omitempty` | `node_id,omitempty` |  |

## internal/types.ToolDefinition

Source: [internal/types/tool.go](../../../internal/types/tool.go#L6).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Name` | `string` | `name` | `name` |  |
| `Description` | `string` | `description,omitempty` | `description,omitempty` |  |
| `Parameters` | `JSONSchemaObject` | `parameters,omitempty` | `parameters,omitempty` |  |
| `Responses` | `[]ToolResponseRule` | `responses,omitempty` | `responses,omitempty` |  |
| `Validate` | `bool` | `validate,omitempty` | `validate,omitempty` |  |
| `ErrorRate` | `float64` | `error_rate,omitempty` | `error_rate,omitempty` |  |

## internal/types.ToolResponseRule

Source: [internal/types/tool.go](../../../internal/types/tool.go#L28).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Match` | `map[string]any` | `match,omitempty` | `match,omitempty` |  |
| `Response` | `any` | `response,omitempty` | `response,omitempty` |  |
| `Error` | `*ToolError` | `error,omitempty` | `error,omitempty` |  |
| `IsDefault` | `bool` | `default,omitempty` | `default,omitempty` |  |

## internal/types.ToolError

Source: [internal/types/tool.go](../../../internal/types/tool.go#L36).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Code` | `string` | `code` | `code` |  |
| `Message` | `string` | `message` | `message` |  |

## internal/types.VectorCollectionDefinition

Source: [internal/types/vector.go](../../../internal/types/vector.go#L8).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `APIVersion` | `string` | `apiVersion` | `apiVersion` |  |
| `Kind` | `string` | `kind` | `kind` |  |
| `Metadata` | `Metadata` | `metadata` | `metadata` |  |
| `Spec` | `VectorCollectionSpec` | `spec` | `spec` |  |

## internal/types.VectorCollectionSpec

Source: [internal/types/vector.go](../../../internal/types/vector.go#L15).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Dimension` | `int` | `dimension` | `dimension` |  |
| `Metric` | `string` | `metric` | `metric` |  |
| `Points` | `[]VectorPoint` | `points,omitempty` | `points,omitempty` |  |
| `Faults` | `VectorFaults` | `faults,omitempty` | `faults,omitempty` |  |

## internal/types.VectorFaults

Source: [internal/types/vector.go](../../../internal/types/vector.go#L22).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Seed` | `int64` | `seed,omitempty` | `seed,omitempty` |  |
| `Rate` | `*float64` | `rate,omitempty` | `rate,omitempty` |  |
| `OperationRates` | `map[string]float64` | `operation_rates,omitempty` | `operation_rates,omitempty` |  |
| `PartialResults` | `*VectorPartialResultsFault` | `partial_results,omitempty` | `partial_results,omitempty` |  |

## internal/types.VectorPartialResultsFault

Source: [internal/types/vector.go](../../../internal/types/vector.go#L31).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `MaxResults` | `int` | `max_results` | `max_results` |  |

## internal/types.VectorPoint

Source: [internal/types/vector.go](../../../internal/types/vector.go#L35).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `ID` | `any` | `id` | `id` |  |
| `Vector` | `[]float64` | `vector` | `vector` |  |
| `Metadata` | `map[string]any` | `metadata,omitempty` | `metadata,omitempty` |  |

## internal/vector.CollectionConfig

Source: [internal/vector/store.go](../../../internal/vector/store.go#L41).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Name` | `string` | `name` | `` |  |
| `Dimension` | `int` | `dimension` | `` |  |
| `Metric` | `Metric` | `metric` | `` |  |
| `PointCount` | `int` | `point_count` | `` |  |

## internal/vector.Point

Source: [internal/vector/store.go](../../../internal/vector/store.go#L48).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `ID` | `string` | `id` | `` |  |
| `ExternalID` | `any` | `-` | `` |  |
| `Vector` | `[]float64` | `vector` | `` |  |
| `Metadata` | `map[string]any` | `metadata,omitempty` | `` |  |

## internal/vector.Match

Source: [internal/vector/store.go](../../../internal/vector/store.go#L55).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `ID` | `string` | `id` | `` |  |
| `ExternalID` | `any` | `-` | `` |  |
| `Score` | `float64` | `score` | `` |  |
| `Metadata` | `map[string]any` | `metadata,omitempty` | `` |  |
| `Vector` | `[]float64` | `-` | `` | Vector is an aligned snapshot of the matched point. Adapters that expose embeddings can project it without a second, racy store lookup. |

## sdk/go/mockagents.JSONRPCEnvelope

Source: [sdk/go/mockagents/mcp.go](../../../sdk/go/mockagents/mcp.go#L30).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `JSONRPC` | `string` | `jsonrpc,omitempty` | `` |  |
| `ID` | `json.RawMessage` | `id,omitempty` | `` |  |
| `Method` | `string` | `method,omitempty` | `` |  |
| `Params` | `map[string]any` | `params,omitempty` | `` |  |

## sdk/go/mockagents.JSONRPCError

Source: [sdk/go/mockagents/mcp.go](../../../sdk/go/mockagents/mcp.go#L40).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Code` | `int` | `code` | `` |  |
| `Message` | `string` | `message` | `` |  |
| `Data` | `any` | `data,omitempty` | `` |  |

## sdk/go/mockagents.TokenUsage

Source: [sdk/go/mockagents/types.go](../../../sdk/go/mockagents/types.go#L11).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `PromptTokens` | `int` | `prompt_tokens` | `` |  |
| `CompletionTokens` | `int` | `completion_tokens` | `` |  |
| `TotalTokens` | `int` | `total_tokens` | `` |  |

## sdk/go/mockagents.ToolCall

Source: [sdk/go/mockagents/types.go](../../../sdk/go/mockagents/types.go#L18).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `ID` | `string` | `id` | `` |  |
| `Name` | `string` | `name` | `` |  |
| `Arguments` | `map[string]any` | `arguments,omitempty` | `` |  |

## sdk/go/mockagents.ChatMessage

Source: [sdk/go/mockagents/types.go](../../../sdk/go/mockagents/types.go#L25).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Role` | `string` | `role` | `` |  |
| `Content` | `string` | `content` | `` |  |
| `ToolCallID` | `string` | `tool_call_id,omitempty` | `` |  |
| `Name` | `string` | `name,omitempty` | `` |  |

## sdk/go/mockagents.ChatResponse

Source: [sdk/go/mockagents/types.go](../../../sdk/go/mockagents/types.go#L35).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Content` | `string` | `content` | `` |  |
| `Model` | `string` | `model` | `` |  |
| `ToolCalls` | `[]ToolCall` | `tool_calls,omitempty` | `` |  |
| `FinishReason` | `string` | `finish_reason,omitempty` | `` |  |
| `Usage` | `TokenUsage` | `usage` | `` |  |
| `Raw` | `any` | `raw,omitempty` | `` |  |
| `StatusCode` | `int` | `status_code` | `` |  |
| `LatencyMs` | `float64` | `latency_ms` | `` |  |

## sdk/go/mockagents.AgentSummary

Source: [sdk/go/mockagents/types.go](../../../sdk/go/mockagents/types.go#L47).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Name` | `string` | `name` | `` |  |
| `Description` | `string` | `description,omitempty` | `` |  |
| `Model` | `string` | `model` | `` |  |
| `Protocol` | `string` | `protocol` | `` |  |
| `ScenarioCount` | `int` | `scenario_count` | `` |  |
| `ToolCount` | `int` | `tool_count` | `` |  |
| `Tags` | `[]string` | `tags,omitempty` | `` |  |

## tools/benchguard.Result

Source: [tools/benchguard/main.go](../../../tools/benchguard/main.go#L51).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Name` | `string` | `name` | `` |  |
| `Iterations` | `int64` | `iterations` | `` |  |
| `NsPerOp` | `float64` | `ns_per_op` | `` |  |
| `BytesPerOp` | `int64` | `bytes_per_op,omitempty` | `` |  |
| `AllocsPerOp` | `int64` | `allocs_per_op,omitempty` | `` |  |
| `OpsPerSecond` | `float64` | `ops_per_second` | `` |  |

## tools/benchguard.Report

Source: [tools/benchguard/main.go](../../../tools/benchguard/main.go#L61).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `SchemaVersion` | `string` | `schema_version` | `` |  |
| `GoVersion` | `string` | `go_version` | `` |  |
| `GOOS` | `string` | `goos` | `` |  |
| `GOARCH` | `string` | `goarch` | `` |  |
| `Package` | `string` | `package` | `` |  |
| `Results` | `[]Result` | `results` | `` |  |

## tools/benchreport.Result

Source: [tools/benchreport/main.go](../../../tools/benchreport/main.go#L35).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Name` | `string` | `name` | `` |  |
| `Iterations` | `int64` | `iterations` | `` |  |
| `NsPerOp` | `float64` | `ns_per_op` | `` |  |
| `BytesPerOp` | `int64` | `bytes_per_op,omitempty` | `` |  |
| `AllocsPerOp` | `int64` | `allocs_per_op,omitempty` | `` |  |
| `OpsPerSecond` | `float64` | `ops_per_second` | `` |  |

## tools/benchreport.Report

Source: [tools/benchreport/main.go](../../../tools/benchreport/main.go#L46).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `SchemaVersion` | `string` | `schema_version` | `` |  |
| `Timestamp` | `time.Time` | `timestamp` | `` |  |
| `GoVersion` | `string` | `go_version` | `` |  |
| `GOOS` | `string` | `goos` | `` |  |
| `GOARCH` | `string` | `goarch` | `` |  |
| `Package` | `string` | `package` | `` |  |
| `Results` | `[]Result` | `results` | `` |  |

## tools/handoffcatalog.exclusion

Source: [tools/handoffcatalog/coverage.go](../../../tools/handoffcatalog/coverage.go#L12).

| Go field | Go type | JSON tag | YAML tag | Source field note |
| --- | --- | --- | --- | --- |
| `Pattern` | `string` | `pattern` | `` |  |
| `Source` | `string` | `source` | `` |  |
| `Reason` | `string` | `reason` | `` |  |
| `Contract` | `string` | `contract` | `` |  |
