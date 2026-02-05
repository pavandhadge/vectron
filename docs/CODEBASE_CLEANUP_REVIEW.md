# Codebase Cleanup Review (Pre-Delete)

This review lists **only** items that look outdated, irrelevant, or purely runtime-generated. Each item includes context so we can safely decide what to delete. I did **not** mark anything that appears functional or still part of the roadmap.

## 1) Runtime/Build Artifacts (Safe to delete, re-generated)

These are produced by tests or builds and are not needed in source control. They can be deleted and re-created by the normal workflows.

- `temp_vectron/`  
  Context: Created by E2E runs. Contains transient `data/` and `logs/` directories.  
  Evidence: folder exists at repo root and is created in tests (see E2E helpers).

- `temp_vectron_benchmark/`  
  Context: Created by benchmark runs. Contains transient `data/` and `logs/`.  
  Evidence: folder exists at repo root and benchmark tests create temp dirs.

- `bin/`  
  Context: Output of build targets (e.g., `./bin/worker`, `./bin/apigateway`).  
  Evidence: present at repo root, used by tests/scripts as build output.

- `main.test`  
  Context: Go test binary artifact.  
  Evidence: present at repo root, large binary.

- `e2e-test.log`, `ultimate_e2e_test_output.log`, `run-all.log`  
  Context: Log artifacts produced by tests/run scripts.  
  Evidence: repo root log files with timestamps.

## 2) Generated Proto Output (Keep, but should be regenerated via script)

These are generated and should not be hand-edited. They are useful but can be regenerated if needed. **Not recommending deletion** unless we want to rely fully on generation in CI.

- `clientlibs/js/proto/**`  
  Context: TS proto output used by the JS client.  
  Evidence: `clientlibs/js/src/client.ts` imports from `clientlibs/js/proto/...`.

- `clientlibs/python/vectron_client/proto/**`  
  Context: Python proto output used by the Python client.  
  Evidence: `clientlibs/python/vectron_client/client.py` imports from `vectron_client/proto/...`.

## 3) TODOs and Roadmap Items (Not outdated)

These are active development signals and **should not** be deleted.

- `reranker/TODO.md`  
  Context: Explicit roadmap for reranker service.

- TODOs in code (examples):  
  - `reranker/cmd/reranker/main.go` (LLM/RL strategies)  
  - `apigateway/internal/translator/translator.go` (payload/metadata translation)  
  - `placementdriver/internal/fsm/fsm.go` (replicationFactor config)

## 4) Docs and Summaries

I did not find any docs that are clearly outdated without deeper product context. If you suspect a specific doc is stale, point me to it and I’ll re-check.

## 5) Disabled / Dead Go Files (Safe to delete or re-enable)

These files are **fully commented out** and are not compiled or executed by any tests or builds.

- `placementdriver/placementdrivertest/integration_test.go`  
  Context: Entire file is commented out (`// package ...` and all code).  
  Evidence: No `package` declaration; `go list` reports it as invalid unless `-e` is used.

## 6) Not Referenced by Build or Docs (Needs Confirmation)

These are not referenced by the Makefile, scripts, or docs, but **could** be local tooling. Please confirm before deletion.

- `worker/cmd/test_storage/`  
  Context: Standalone `main` package not built by `Makefile` and not referenced anywhere in repo.  
  Evidence: `rg "test_storage"` returns no references in scripts/docs.

---

### Summary
Safe deletions are limited to **runtime/build artifacts** listed in section 1 and the **fully disabled** test file in section 5. Everything else appears either **actively used** or **generated inputs** for SDKs.
