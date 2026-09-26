# Runtime log correlation

LSP request and notification lifecycles carry `spanId` and `traceId`. A span identifies one execution, including overlapping executions of the same method. `parentSpanId` identifies the operation that initiated a child. Diagnostic work retains this correlation when a document notification hands work to the background revision worker; its existing cancellation lifetime remains independent of the notification.

The diagnostic hierarchy is `LSP operation → check.total → check.diagnostics → individual checks`. Parsing and diagnostic database lookup are also children of `check.total`. `check.prepare` measures shared syntax, include-document and external-global preparation before the individual checks. These fields appear both in debug-file metadata and readable Output logs, including completion and cancellation records.

The log analyzer pairs lifecycle records by trace and span IDs when present. Explicit parent IDs work without timestamps, across document URIs, and after an initiating asynchronous operation has completed. Missing, duplicate or cyclic parent information does not justify inventing a relationship. Older records without IDs retain the existing URI/time or log-order inference, shown with dashed edges. Operations not yet carrying correlation fields continue to use that legacy path.

Parent and child durations overlap: their sum is not elapsed wall time or CPU time. Diagram spacing follows log order and does not imply a measured duration for untimed records. The detail view explains all three correlation fields in Japanese and English.
