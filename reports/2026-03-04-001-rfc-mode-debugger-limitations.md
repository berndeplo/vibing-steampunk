# RFC Mode Debugger: Root Cause & Fix

**Date:** 2026-03-04
**Report ID:** 001
**Subject:** Why ABAP debugging failed over RFC and how it was fixed
**Related Documents:** [ADR-004 JCo RFC Sidecar](../docs/adr/004-jco-rfc-sidecar.md), [RFC Mode Docs](../docs/rfc-mode.md)

---

## Problem Statement

When vsp is connected via RFC mode (JCo sidecar → `SADT_REST_RFC_ENDPOINT`), the `DebuggerListen` tool failed with:
```
sidecar request failed (is the sidecar running at http://localhost:8081?):
context deadline exceeded (Client.Timeout exceeded while awaiting headers)
```

## Root Cause

**NOT a SAP limitation.** `SADT_REST_RFC_ENDPOINT` handles long-polling fine — Eclipse ADT debugs over pure RFC without any HTTP fallback.

The actual bug was in `pkg/adt/rfc_transport.go`:

```go
// Line 58 — BEFORE (broken)
httpClient: &http.Client{Timeout: cfg.Timeout},  // 60 seconds hard cap
```

The `RfcTransport` had a **60-second hard timeout** on the HTTP client talking to the sidecar. The debugger listener sets a context deadline of `timeout + 30s` (e.g., 90 seconds for a 60s listener), but `http.Client.Timeout` is a hard cap that **overrides the context deadline**. The vsp→sidecar HTTP call died at 60s before the SAP-side listener could respond.

Proof: manually calling the sidecar with a 5-second listener timeout worked perfectly:
```bash
curl -X POST http://localhost:8081/rfc-proxy \
  -H 'Content-Type: application/json' \
  -d '{"method":"POST","uri":"/sap/bc/adt/debugger/listeners?timeout=5&..."}'
# → {"statusCode":200,"body":""}  ← normal timeout, empty body = no debuggee
```

## Fix

```go
// Line 58 — AFTER (fixed)
httpClient: &http.Client{},  // No hard timeout — context deadline controls per-request timeouts
```

This matches how the HTTP `Transport` works — it also uses no `http.Client.Timeout`, relying purely on context deadlines for per-request timeout control. The debugger listener already sets an appropriate context deadline (`timeout + 30s`), and other callers set their own deadlines as needed.

## SAP Debugger Architecture Reference

For future reference, the SAP debugger has 3 layers:

| Layer | Components | Package |
|-------|-----------|---------|
| **ADT REST** | `CL_TPDA_ADT_RES_LISTENERS`, `..._DEBUGGER`, `..._VARIABLES`, `..._STACK`, `..._ACTIONS`, `..._BREAKPOINTS`, `..._WATCHPOINTS`, `..._STATEMENTS`, `..._SYSTEM_AREAS`, `..._MEM_SIZES` | `STPDA_ADT` |
| **TPDA API** | `CL_TPDAPI_SERVICE` (singleton), `IF_TPDAPI_SESSION`, `IF_TPDAPI_CONTROL_SERVICES`, `IF_TPDAPI_DATA_SERVICES`, `IF_TPDAPI_BP_SERVICES`, `IF_TPDAPI_WP_SERVICES`, `IF_TPDAPI_STACK_HANDLER` | `STPDA_API` |
| **ADI Kernel** | Function group `STPDA_ADI` | `STPDA_ADI` |

### TPDA API Method Mapping

| Debugging Operation | TPDA API Method | ADT REST Endpoint |
|-------------------|----------------|-------------------|
| Listen (terminal) | `CL_TPDAPI_SERVICE->start_listener_for_terminal_id()` | `POST /debugger/listeners` |
| Listen (user) | `CL_TPDAPI_SERVICE->start_listener_for_user()` | `POST /debugger/listeners` |
| Stop listener | `CL_TPDAPI_SERVICE->stop_listener_for_*()` | `DELETE /debugger/listeners` |
| Check conflict | `CL_TPDAPI_SERVICE->check_listener_conflict_*()` | `GET /debugger/listeners` |
| Get waiting debuggees | `CL_TPDAPI_SERVICE->get_waiting_debuggees()` | (internal) |
| Attach | `CL_TPDAPI_SERVICE->attach_debuggee()` | `POST /debugger/sessions` |
| Step | `IF_TPDAPI_CONTROL_SERVICES->step_*()` | `POST /debugger/sessions` (action) |
| Get stack | `IF_TPDAPI_SESSION->get_stack_handler()` | `POST /debugger/sessions` (action) |
| Get variables | `IF_TPDAPI_SESSION->get_data_services()` | `POST /debugger/sessions` (action) |

### Test Function Modules (STPDA_API_TEST)

SAP ships RFC-enabled test FMs (internal testing only):
- `TPDAPI_TEST_DEBUGGER` — "Test Debugger API: Debugger Modus"
- `TPDAPI_TEST_DEBUGGEE` — Debuggee test (RFC-enabled)
- `TPDAPI_TEST_START_LISTENER` — Listener test (RFC-enabled)
- `TPDAPI_TEST_RFC` — RFC test

## Key Lesson

**Always verify assumptions before concluding a SAP limitation.** The initial investigation incorrectly concluded that `SADT_REST_RFC_ENDPOINT` couldn't handle long-polling. In reality, Eclipse ADT uses it for debugging over pure RFC. The issue was a simple client-side timeout misconfiguration in our Go code.
