# Codebase Cleanup Review (Current)

Last updated: 2026-02-08

This is an updated cleanup posture document (not a historical review).

## 1. Current State

- Core service structure is coherent across modules (`apigateway`, `worker`, `placementdriver`, `auth/service`, `reranker`).
- Proto contracts are centralized under `shared/proto`.
- Runtime tuning variables are documented in `ENV_SAMPLE.env`.

## 2. Documentation Cleanup Status

- `docs/` has been refreshed with code-aligned content.
- Legacy speculative content was replaced or converted to clearly marked planning/backlog sections.
- Source-of-truth references are included in each major doc.

## 3. Remaining Cleanup Opportunities

1. Keep root `README.md` aligned with `docs/CURRENT_STATE.md` on every major feature change.
2. Periodically prune outdated TODO comments that no longer match implementation.
3. Consolidate overlapping status trackers when they drift.

## 4. Safety Rules for Future Cleanup

- Do not remove generated artifacts unless generation paths are validated.
- Do not delete TODO/plan files that are actively referenced by current workstreams.
- Prefer conversion to `archived`/`superseded` notes over silent deletion.

## 5. Source of Truth

- `docs/CURRENT_STATE.md`
- `README.md`
- `AI_ONBOARDING.md`
