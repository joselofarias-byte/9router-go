# Public / commercial edition boundary

This repository remains the open-source Community/core source tree.

## Rule

Public code may define and consume **stable contracts** for product capabilities,
license verification and edition status. Proprietary implementations, commercial
control-plane logic, billing, customer administration, anti-abuse heuristics and
private release credentials do not belong in this repository.

The public boundary is `pkg/edition`.

Community must remain independently buildable and useful. If no commercial
provider is assembled into a release, all commercial capabilities fail closed.

## What belongs here

- the MIT-derived gateway/data plane and retained notices;
- provider adapters, protocol translation, streaming and baseline routing;
- Community UX and local/self-hosted functionality;
- public capability names and edition contracts;
- public-key cryptographic verification and signed lease formats;
- tests that prove invalid or absent entitlements fail closed;
- documentation needed to build and contribute to Community.

## What does not belong here

- issuer/signing private keys;
- payment-provider secrets or customer records;
- production admin tokens;
- private licensing/control-plane implementation;
- proprietary Pro/Business algorithms created outside this public tree;
- anti-abuse rules whose disclosure would materially weaken enforcement;
- private release/signing infrastructure.

## Security model

Security must not depend on hiding the verifier or the wire format. Public-key
verification can be audited publicly. The authority remains outside the client:
a public build cannot mint a valid server lease because it does not possess the
issuer private key or the commercial control-plane authority.

A patched client may lie about local UI state. High-value paid behavior should
therefore depend on a verified capability and, where appropriate, on a
server/service or proprietary implementation that the Community repository does
not contain.

## Licensing

The upstream project is MIT licensed. The inherited MIT copyright and permission
notice must remain in copies or substantial portions of the inherited software.

Code already published in this repository under MIT should be treated as
publicly available. The commercial boundary is primarily a rule for **new
original implementations** and private services going forward, not an attempt
to retroactively make public source private.

This document is an engineering policy, not legal advice.
