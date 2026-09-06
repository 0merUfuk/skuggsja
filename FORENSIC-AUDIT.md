# History coverage audit

This public summary records generalizable findings from independent storage discovery and adapter review. The original host inventory, timestamps, warning counts, source fingerprints and complete forensic record were preserved byte-for-byte in private evidence before this replacement. No personal history or aggregate is included here.

## Discovery findings and repairs

History discovery must account for configured roots, canonical roots, native alternate homes, nested transcripts and recovery directories. An account or organization identifier in configuration is evidence to investigate, not proof of a separate conversation store.

Claude discovery now retains configured and canonical homes and recognizes immediate native `.claude-*` sibling homes. Nested agent transcripts are included. Same-identity complete or partial file copies are coalesced without adding duplicate sessions or rhythm events, while unique prompt, response and tool IDs remain available. An explicit source scope bypasses automatic native discovery.

Codex discovery includes recovery directories and validates pagination boundaries. Byte-identical same-ID copies are suppressed before pagination validation; suppressed plain/compressed siblings remain in the byte-hashed audit set. Finding an additional copy does not imply additional usage.

The independent search distinguished transcript stores from indexes, application account/organization metadata, auxiliary databases, backups, archives, logs and caches. Claude Desktop Code indexes supply coverage evidence; Cowork/local-agent transcripts are not Claude Code usage. Repository configuration and account catalogs do not automatically become source histories.

Archive enumeration, protected locations, opaque/encrypted content, nested archive members, symlink targets and unmounted stores limit a disk inventory. Unknown internal dates remain unknown. A bounded search cannot prove that no older detail exists anywhere.

## Coverage and child-session conclusions

The former exact-depth Claude transcript rule excluded nested subagent transcripts before parsing. `isSidechain` was present in source data; a zero child count did not establish its absence. Nested children and direct physical sidechain sessions are now recognized and kept separate from owner totals.

Prompt history and indexes can predate surviving detailed transcripts. Those dates establish coverage gaps, not recoverable token, tool, model or response metrics. Missing Codex rollouts were not recovered merely by widening discovery. A corrupt supplemental history database could not be used to rule out older detail. Account switching was not established as the cause of missing history; no numbers were adjusted to fit a recollection.

The UI presents coverage independently for each harness, including when provider details are collapsed. It distinguishes history-only sessions, references without surviving detail and unavailable dates. Coverage confidence describes the assessment of surviving data, not lifetime completeness or low usage.

## Claude warning semantics

The original counted warning ledger is private. These codes explain what each diagnostic combines, excludes or retains; the list describes the observed audit categories rather than promising that no other schema warning can occur.

| Warning code | Meaning |
| --- | --- |
| `aggregate_history_predates_detail` | Aggregate coverage evidence predates surviving detail; no detailed usage is inferred. |
| `assistant_updates_merged` | Repeated assistant updates are combined by response ID while retaining distinct tool calls. |
| `assistant_usage_advanced` | A later cumulative output-usage snapshot replaces an earlier snapshot. |
| `code_session_index_reconciled` | Desktop Code references are reconciled with surviving local sessions without adding duplicates. |
| `compact_summary_prompts_excluded` | Compaction summaries are excluded from owner prompt metrics. |
| `conflicting_usage` | Copied response IDs disagree; one deterministic concrete source record is retained. |
| `copied_prompts` | Repeated fork-history prompt IDs are counted once. |
| `copied_responses` | Repeated response IDs are counted once; distinct tool IDs are retained. |
| `desktop_local_agent_sessions_excluded` | Cowork/local-agent transcripts are audited but excluded from Claude Code usage. |
| `desktop_session_references_unmapped` | Bridge/fork references with another identity namespace are not treated as missing Claude Code sessions. |
| `duplicate_tool_use` | Repeated tool-use IDs within a surviving transcript are counted once. |
| `global_state_only_sessions` | Global-state references supply coverage evidence without invented usage. |
| `history_only_sessions` | Prompt-history evidence survives without a primary transcript; it supplies no absent model/token/tool/response detail. |
| `history_records_reconciled` | Prompt-history rows are suppressed for session IDs with surviving detail; partial detail can still omit unique prompts. |
| `meta_prompts_excluded` | Metadata user records are excluded from owner prompt metrics. |
| `owner_prompt_records_excluded` | Records matching any non-owner filter are excluded; this is the union of overlapping filters. |
| `physical_session_time_unavailable` | A physical session is retained without inventing a timestamp for span or rhythm metrics. |
| `project_index_only_sessions` | Index-only references supply coverage evidence without adding usage. |
| `source_tool_prompts_excluded` | Tool-generated user records are excluded from owner prompt metrics. |
| `tool_result_prompts_excluded` | Tool-result user records are excluded from owner prompt metrics. |
| `transcript_only_prompts_excluded` | Transcript-only user records are excluded from owner prompt metrics. |

Prompt-exclusion categories overlap; summing their individual counts would overstate loss. Assistant-update merging retains concrete usage snapshots and distinct tools rather than removing historical sessions.

## Copied-history deduplication

The processing path is explicit:

1. Parse physical transcripts and combine repeated assistant updates within each transcript.
2. Coalesce complete/partial files sharing session identity.
3. Suppress prompt-history rows for IDs with surviving detailed sessions.
4. Deduplicate copied fork prompt, response and tool IDs in analytics.

Metric deduplication does not remove a whole physical session. Conflicting copied response usage retains a deterministic concrete record and warns. Whole-session history reconciliation remains conservative: a partial transcript can omit unique older prompts, but timestamp offsets and command records do not support an exact automatic merge. The tool does not estimate missing activity.

## Verification boundaries

Discovery, classification and deduplication fixes have regression coverage. Live-source generation was separately checked under source-write and remote-network denial. Source changes during generation remain neutral activity observations and do not identify the writer.

The release equality record is distinct: both complete-scope direct invocations produced **0/8** equal windows; the explicitly labeled Claude/Cursor fallback passed on its third attempt (**1/3**), with Hermes live equality unmeasured. The verifying agent's own Codex store was separately snapshotted and excluded to avoid its self-generated store activity. No other ad hoc narrowing or live harness intervention was used.

[VERIFICATION.md](VERIFICATION.md) records test/build/browser results and the precise limits of the read-only and equality claims. Real reports and screenshots remain private, and no original source was repaired or reconstructed.
