# Session Not Found — Root Cause Investigation

**Date:** 2026-03-24
**Report ID:** 001
**Subject:** HTTP session failures on S4T system — root cause analysis and fixes
**Related Documents:** commit `610bfeb` (auto-reconnect), commit `d73460a` (401 retry)

---

## Context

After connecting to `http://s4t-app.g-hana-coe.accenture.com:8080`, every MCP tool call after the first one failed with "400 Session not found". Three changes were made to fix this. This report investigates whether the server code was actually broken or if this was a pre-existing issue.

## Root Cause: The `Secure` Cookie Flag Over HTTP

The SAP S4T system sets session cookies with the `Secure` flag:

```
set-cookie: SAP_SESSIONID_S4T_100=...; path=/; secure; HttpOnly
```

But the connection URL is `http://` (not `https://`). Go's `cookiejar` follows RFC 6265 strictly and **refuses to send `Secure` cookies over plain HTTP**. Result: every request after the first arrives at SAP without a session cookie → "Session not found".

**This is NOT a regression.** The cookie jar code in `config.go` has been unchanged since the initial commit — it always used a plain `cookiejar.New(nil)`. The secure cookie problem was always latent.

## Why It Was Never Noticed Before

The git history and reports tell the story:

- **Main development** (Alice V / ooisee) was done against `http://a4h.desude.su:50000` and `https://` systems
- **All documentation examples** use `https://your-sap:44300`
- **The S4T system** is accessed via `http://` on port 8080

The A4H system likely either:

1. Doesn't set the `Secure` flag on cookies over HTTP, or
2. Was accessed over HTTPS where `Secure` cookies work fine

The S4T system at Accenture is configured to set `Secure` on all session cookies regardless of protocol — a common SAP default that only causes problems over HTTP.

## Three Changes Made

### Change 1: Add "session not found" to `IsSessionExpired()` — Genuine gap

**File:** `pkg/adt/http.go`

The original detection only covered:

- `ICMENOSESSION` (SAP kernel XML error)
- `Session Timed Out` (XML error)
- `Session no longer exists` (XML error)

The S4T system returns an **HTML error page** with "Session not found" — a different ICM response format. This is a valid session expiry variant that the original code missed. Other users on different SAP systems could hit this too.

```go
// Before
return strings.Contains(msg, "icmenosession") ||
    strings.Contains(msg, "session timed out") ||
    strings.Contains(msg, "session no longer exists")

// After
return strings.Contains(msg, "icmenosession") ||
    strings.Contains(msg, "session timed out") ||
    strings.Contains(msg, "session no longer exists") ||
    strings.Contains(msg, "session not found")
```

### Change 2: `clearCookieJar()` on session recovery — Bug fix for retry logic

**File:** `pkg/adt/http.go`

The existing retry logic (from commit `610bfeb`) cleared the CSRF token and session ID but left **stale cookies in the Go HTTP client's cookie jar**. Even when `IsSessionExpired` correctly detected the error, the retry would re-send the expired session cookie via the jar, causing the retry to fail with the same error.

Added `clearCookieJar()` method that replaces the cookie jar with a fresh one before re-authenticating. Handles both the standard `cookiejar` and the `insecureCookieJar` wrapper.

### Change 3: `insecureCookieJar` wrapper for HTTP — The actual root cause fix

**File:** `pkg/adt/config.go`

This is the real fix. Without it, Go's cookie jar silently drops the SAP session cookie because it's marked `Secure` on an `http://` connection. No amount of retry logic can fix a cookie that's never sent.

When the base URL uses `http://`, the cookie jar is wrapped with `insecureCookieJar` which strips the `Secure` flag from incoming cookies so they are stored and sent back on subsequent HTTP requests.

```go
type insecureCookieJar struct {
    jar http.CookieJar
}

func (j *insecureCookieJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
    for _, c := range cookies {
        c.Secure = false
    }
    j.jar.SetCookies(u, cookies)
}
```

## Summary

| Change | Was it broken before? | Who's affected? |
|--------|----------------------|-----------------|
| "session not found" detection | Yes — gap since `610bfeb` (Dec 2025) | Any SAP system returning HTML 400 errors |
| Cookie jar clearing on retry | Yes — bug since `610bfeb` (Dec 2025) | Any system where sessions expire mid-use |
| Secure cookie over HTTP | Yes — gap since initial commit | **Any HTTP connection** to SAP systems that set `Secure` flag |

## Verdict

All 3 changes fix real bugs. The code was never fully tested with a plain HTTP SAP connection that sets `Secure` cookies. The S4T system simply exposed latent issues that HTTPS connections masked.

## Files Modified

- `pkg/adt/http.go` — "session not found" detection, `clearCookieJar()` method, cookie jar clearing in retry paths
- `pkg/adt/config.go` — `insecureCookieJar` wrapper, applied when base URL is `http://`
