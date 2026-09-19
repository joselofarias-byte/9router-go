# AI Free Routing Fabric

This fork is not a generic copy of `luqman-v1/9router-go`. It keeps the upstream data plane and adds a control plane that discovers, verifies, scores, and routes across free or low-cost providers for OpenCode and other coding agents.

## Decision: `fabric-free` and `free-best` are the same pool

`fabric-free`, `free-best`, and `free` are public names for one logical pool.

| Name | Role |
|------|------|
| `fabric-free` | Canonical pool ID used by the control plane, admin APIs, and docs. |
| `free-best` | Virtual model for OpenCode and other clients. |
| `free` | Short alias for the same pool. |

They are **not** two implementations. Both expand live against the current registry snapshot:

1. Keep only `PricingMode` `free` or `free_tier`.
2. Require an active provider, model, and account.
3. Drop quarantined, quota-exhausted, and expired-session nodes.
4. Score remaining nodes with trust, observed success rate, recent latency, free bonus, and account-risk penalty.
5. Return an ordered combo-style fallback list. The existing data-plane combo path then fails over.

This is **not** a hard-coded provider list. Membership changes when discovery, probes, or live traffic update the snapshot and trust manager.

## Control plane

```text
Discovery adapters
  Cline free catalog (always on)
  Models.dev (free/zero-cost rows only)
  Kira public catalog (always on)
  Kiro static free set (always on; routable only with a Kiro account)
  UnoRouter :free suffix (opt-in: UNOROUTER_API_KEY)
  OrcaRouter (opt-in: ORCAROUTER_BASE_URL + ORCAROUTER_API_KEY)
        |
        v
Registry snapshot (atomic activate + last-known-good rollback)
        |
        +-- account sync from providerConnections (no credentials in snapshots)
        +-- trust / quota / session / latency
        +-- scoring + policy
        v
resolveModel("free-best"|"fabric-free"|"free")
        |
        v
existing combo fallback + locks + SSE
```

### Policies

| Policy | Use |
|--------|-----|
| `free-only` | Forced for the unified free pool. |
| `balanced` | Default for exact model IDs. |
| `free-first` | Exact model IDs with a free-tier boost. |
| `trusted-only` | Verified/trusted nodes only. |

Override the exact-model default with `FABRIC_ROUTING_POLICY`.

### Admin (API-key protected)

- `GET /admin/registry` — active snapshot (no credentials)
- `GET /admin/explain-route?model=&policy=`
- `GET /admin/fabric/status?pool=fabric-free`
- `POST /admin/fabric/probe?provider=&model=&account=`

## What is not claimed

- Probes are on-demand, not a background hammer against every account.
- Models.dev no longer dumps thousands of unknown-price rows into the Fabric snapshot.
- Real Termux DNS/TLS and live WorkBuddy/CodeBuddy sessions still need a phone and real accounts.
