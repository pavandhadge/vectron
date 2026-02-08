# Vectron Docs (Current)

Last updated: 2026-02-08

This folder is maintained as current-state documentation.

## Primary Website-Ready Docs

- `docs/Vectron_Project_Overview.md`
- `docs/ARCHITECTURE_DEEP_DIVE.md`
- `docs/PHILOSOPHY_AND_DIRECTION.md`
- `docs/API.md`

## System State and Reference

- `docs/CURRENT_STATE.md`: code-verified architecture, APIs, runtime config, and known gaps.
- `docs/Vectron_Architecture.md`: concise architecture and request flow summary.

## Service Docs

- `docs/APIGateway_Service.md`
- `docs/Auth_Service.md`
- `docs/PlacementDriver_Service.md`
- `docs/Worker_Service.md`
- `docs/Feedback_System.md`
- `docs/APIGateway_Reranker_Integration.md`

## Operations and Planning

- `docs/System_Integration.md`
- `docs/PERFORMANCE_OPTIMIZATION_BACKLOG.md`
- `docs/DISTRIBUTED_SYSTEM_PLAN.md`
- `docs/RERANKER_OPTIMIZATION.md`
- `docs/CODEBASE_CLEANUP_REVIEW.md`

## Canonical Technical References

- `shared/proto/apigateway/apigateway.proto`
- `shared/proto/auth/auth.proto`
- `shared/proto/placementdriver/placementdriver.proto`
- `shared/proto/worker/worker.proto`
- `shared/proto/reranker/reranker.proto`
- `ENV_SAMPLE.env`

## Rule

If documentation conflicts with code, treat proto contracts and runtime entrypoint code as authoritative.
