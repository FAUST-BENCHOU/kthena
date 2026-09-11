---
title: Session Sticky (Session Affinity) for Kthena Router
authors:
- "@FAUST-BENCHOU"
reviewers:
- TBD
approvers:
- TBD

creation-date: 2026-04-01

---

## Session Sticky (Session Affinity) for Kthena Router

### Summary

This proposal specifies **session sticky** (session affinity) for **Kthena Router**: requests that present the same **session key** on a `ModelRoute` are routed to the same **ModelServer Pod**, for as long as the mapping is valid under TTL and the Pod remains selectable. The feature is **opt-in** on `ModelRoute` (per-route **sources** and **TTL** only). The mapping store is **in-process memory** or **shared Redis**, selected by **router configuration**, not by the `ModelRoute` API. **Failover** clears or replaces stale bindings when the mapped target is no longer selectable.

### Motivation

Session stickiness matters for AI inference when operators need to:

- Preserve **conversation context** across multiple HTTP requests.
- Keep **model or cache state** on a specific instance for related calls.
- Reduce latency by **reusing instance-local caches**.

Kubernetes Service `sessionAffinity: ClientIP` only keys on **source IP**. Affinity is therefore defined at the **Kthena Router** layer: bind a session key to a backend for a **TTL**, then re-select when the binding is missing, expired, or invalid.

#### Goals

1. Session sticky is implemented in **Kthena Router**, integrated with `ModelRoute` matching and existing scheduling.
2. **Backward compatibility**: if **`spec.sessionSticky` is nil/omitted**, behavior is unchanged.
3. **Session key** can be derived from **HTTP headers**, **query parameters**, **cookies**, or **JWT claims**, using an ordered list of sources.
4. When a mapping exists and the target is still selectable, **the same session key routes to that ModelServer Pod**.
5. **Declarative configuration** via optional **`ModelRoute.spec.sessionSticky`**. Store backend is **not** on the CRD.
6. **Horizontal scaling**: all router replicas share the same Redis store when Redis is configured.
7. **Failover**: if the mapped Pod (or ModelServer) is not available, the stale mapping is removed, a new target is chosen, and the event is logged.

#### Non-goals (this version)

- Session sticky for **PD disaggregation**. If any resolved target has `WorkloadSelector.PDGroup`, sticky is **bypassed at runtime** and a warning is logged.
- **Future work**: PD-specific session sticky semantics in a follow-up proposal.

### Proposal

#### User stories

**Story 1** — An operator sets **`spec.sessionSticky`** on a `ModelRoute` with a header source `X-Session-ID`. Requests with the same header value hit the same ModelServer Pod until TTL expires or that Pod leaves the endpoint set.

**Story 2** — An operator runs **multiple Kthena Router replicas** with **Redis** enabled in router config (not on `ModelRoute`). The same session key sticks to the same Pod regardless of which replica handles the request.

#### Architecture

Session sticky is **not** a scheduler plugin. It only **looks up a binding** before `Schedule`, **pins one Pod after Filter**, then **writes the chosen Pod** after `Schedule`. Filter and Score plugins are unchanged.

**Request path**

1. Match `ModelRoute`. If `sessionSticky` is set, take the first non-empty source (`Header` / `Query` / `Cookie` / `JWTClaim`) as the session key. Empty key: skip sticky. A `JWTClaim` source requires JWT auth on that route (admission rejects otherwise).
2. `Get` binding. If present, rematch the route target to `binding.ModelServer` and pass `StickyPodName = binding.Pod` into `Schedule`.
3. `Schedule` (aggregated, non-PD): **Filter plugins** (e.g. least-request) run on the full candidate list → if `StickyPodName` is still in that list, shrink it to that one Pod → **Score plugins** (e.g. least-latency, prefix-cache) run on whatever list remains.
4. After a Pod is chosen, `Commit` `{ModelServer, Pod}` with the route TTL (store details below). Then proxy.

**How this interacts with other scheduling factors**

| Case | What happens |
|------|----------------|
| Sticky Pod **survives Filter** | Candidate list becomes that one Pod. Score still runs, but cannot pick a different Pod. |
| Sticky Pod **fails Filter** (overloaded, gone, …) | Pin is dropped. Score ranks the remaining Pods as usual. The new winner is committed. |
| No binding / empty session key | Filter then Score as today. First successful Pod is committed if a key exists. |
| **PD** (`PDGroup` set) | Sticky is skipped (no pin, no commit). PD Filter/Score is unchanged. |

Pinning is **after Filter, before Score**. Other plugins are not reordered and do not need sticky-specific logic.

#### Session map storage

The store is a TTL map: **opaque key → Binding `{modelServer, pod}`**.

| Item | Value |
|------|--------|
| Key | `kthena/sticky/` + `sha256(namespace/name\|sessionKey)` (`ModelRoute` identity + hashed session material; raw session key is not stored in the Redis/memory key) |
| Value | ModelServer **short name** (same namespace as the `ModelRoute`) and **Pod name** |
| TTL | `ModelRoute.spec.sessionSticky.sessionAffinitySeconds` (default **10800**); each successful request **Set/Commit**s the full TTL (sliding expiry) |
| API | `Get` / `Delete` / `Commit`; no separate Refresh RPC |

**Memory** (default): process-local map plus a background sweeper. Suitable for single replica or tests. Replicas do **not** share bindings.

**Redis**: Hash fields `modelServer` and `pod`, plus key TTL. Lua commit is atomic:

- missing → `HSET` + `EXPIRE`, return the new binding
- same binding → `EXPIRE` only (refresh)
- different binding → return the existing fields (do not overwrite)

Router config (not `ModelRoute`):

```yaml
sessionSticky:
  backend: memory   # or redis
  redis:
    address: host:port
```

All replicas in a deployment must use the same backend. Redis mode fails fast at startup if address is missing or unreachable.

#### Notes / constraints

- Binding is scoped per `ModelRoute` (namespace/name in the store key), not cluster-global.
- ModelServer name in the binding is the same-namespace short name; Pod name is unique within that namespace.

#### Risks and mitigations

- **Split brain without Redis**: memory store is per process; multi-replica must use Redis.
- **Stale Pod**: filter miss clears the pin; commit writes the newly selected Pod.
- **Concurrent first request**: Redis Lua returns the first writer; the loser adopts that Pod if it is still selectable.

### Design details

#### `ModelRoute` API

**`sessionSticky` is a pointer** (`omitempty`). Nil/omitted = off; non-nil = on. No `enabled` boolean.

| Field | Purpose |
|-------|---------|
| `sessionAffinitySeconds` | TTL in seconds; optional, default **10800**; minimum **1** when set. |
| `sources` | Ordered list (max **16**) of `SessionKeySource`. **Required and non-empty when `sessionSticky` is non-nil**. |

Root spec: `SessionSticky` is a sibling of `Rules` / `RateLimit` on `ModelRouteSpec`.

```go
type ModelRouteSpec struct {
	ModelName     string                      `json:"modelName,omitempty"`
	LoraAdapters  []string                    `json:"loraAdapters,omitempty"`
	ParentRefs    []gatewayv1.ParentReference `json:"parentRefs,omitempty"`
	Rules         []*Rule                     `json:"rules"`
	RateLimit     *RateLimit                  `json:"rateLimit,omitempty"`
	SessionSticky *SessionSticky              `json:"sessionSticky,omitempty"`
}

type SessionSticky struct {
	SessionAffinitySeconds *int32             `json:"sessionAffinitySeconds,omitempty"`
	Sources                []SessionKeySource `json:"sources,omitempty"`
}

type SessionKeySourceType string

const (
	SessionKeySourceHeader   SessionKeySourceType = "Header"
	SessionKeySourceQuery    SessionKeySourceType = "Query"
	SessionKeySourceCookie   SessionKeySourceType = "Cookie"
	SessionKeySourceJWTClaim SessionKeySourceType = "JWTClaim"
)

type SessionKeySource struct {
	Type SessionKeySourceType `json:"type"`
	Name string               `json:"name"`
}
```

Header names are case-insensitive; Cookie names are case-sensitive.

#### Validation

- **Webhook (`ModelRoute`)**: when `spec.sessionSticky` is non-nil, `sources` must be non-empty; `sessionAffinitySeconds`, if set, must be ≥ 1. No cross-object PD rejection at admission (object lifecycle ordering).
- **Router**: validates memory vs Redis and Redis address/connectivity at startup.

#### Observability

- **Logs**: store errors, failover when mapped Pod is not selectable, Redis issues, PD bypass.
- **Response header (test/debug only)**: `X-Kthena-Backend-Pod` is off by default; enable only via a router debug flag for e2e.

### Test plan

#### Unit tests

- Session key extraction: Header, Query; source ordering; nil `sessionSticky`; all sources empty.
- In-memory store Set/Get/Commit with TTL; Redis commit does not overwrite a different winner.

#### End-to-end acceptance (`test/e2e/router/`)

Scoring plugins that would hide stickiness should be disabled for these cases. E2E that asserts backend identity requires the debug `X-Kthena-Backend-Pod` flag.

| ID | Scenario | Expected outcome |
|----|----------|------------------|
| E2E-SS-01 | Header source; repeated requests with the same session header. | Same Pod. |
| E2E-SS-02 | Two session keys, then reuse the first. | Each key stays on its Pod; first key does not adopt the second. |
| E2E-SS-03 | `sessionSticky` set; header omitted. | No error; spread across at least two Pods. |
| E2E-SS-04 | `sessionSticky` absent; sticky-like header present. | Header does not pin; at least two Pods. |
| E2E-SS-05 | Query source only. | Same query value → same Pod. |
| E2E-SS-06 | Cookie source. | Same cookie → same Pod. |
| E2E-SS-07 | Short TTL; requests before and after expiry. | Sticky within TTL; re-bind after expiry. |
| E2E-SS-08 | Delete the bound Pod; retry same key. | New healthy Pod. |
| E2E-SS-09 | Two router replicas + Redis store. | Same session key → same Pod on both replicas. |
| E2E-SS-10 | `sessionSticky` non-null with empty `sources`. | Admission rejected. |
| E2E-SS-11 | Header and query both present; header listed first. | Header value wins. |
| E2E-SS-12 | PD target at runtime with `sessionSticky` set. | Sticky bypassed; warning log. |

### References

- Kubernetes kube-proxy session affinity (conceptual analog).
- Kthena Router E2E: `test/e2e/router/`.
- Implementation: `pkg/kthena-router/sessionsticky/`, `pkg/kthena-router/router/router.go`, scheduler `StickyPodName` pin in `pkg/kthena-router/scheduler/scheduler_impl.go`.
