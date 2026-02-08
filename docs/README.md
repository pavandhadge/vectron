# Vectron Docs (Current)

Last updated: 2026-02-08

This folder is now maintained as current-state documentation.

## Start Here

- `docs/CURRENT_STATE.md`: Code-verified architecture, APIs, runtime config, and known gaps.
- `docs/Vectron_Project_Overview.md`: concise project overview.
- `docs/Vectron_Architecture.md`: runtime architecture and request flows.

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
