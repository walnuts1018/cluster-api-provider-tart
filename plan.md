# Plan: Addressing PR #87 Review Feedback

## Phase 1: Code Refinement
### 1. Revert incorrect fix for `defer resp.Body.Close()`
- **File:** `test/e2e/provisioning/provisioning_test.go`
- **Action:** Restore `defer resp.Body.Close()` immediately after the error check for `http.Get`. Remove the synchronous `resp.Body.Close()` after `io.Copy`.
- **Rationale:** Technical correctness for connection pooling and panic safety.

### 2. Verify License Headers
- **Files:** `test/e2e/provisioning/provisioning_suite_test.go`, `test/e2e/provisioning/provisioning_test.go`, `test/e2e/provisioning/simulator.go`
- **Action:** Ensure they all have the Apache 2.0 license header. (Already confirmed in previous read for suite and simulator, need to double check `provisioning_test.go`).

## Phase 2: Communication (GitHub Replies)
### 1. Reply to Comment 8 (`defer resp.Body.Close()`)
- **Content:** Explain that `defer` is standard practice for resource safety even if the body is fully read, especially to handle potential panics or early returns from `Expect` calls.
- **Action:** `gh api repos/walnuts1018/cluster-api-provider-tart/pulls/87/comments/3179858500/replies`

### 2. Reply to Comment 11 (`${PWD}` in YAML)
- **Content:** Point out that `mise.toml` uses `envsubst` to expand these variables into a temporary file before the test starts, so the literal `${PWD}` never reaches the clusterctl framework.
- **Action:** `gh api repos/walnuts1018/cluster-api-provider-tart/pulls/87/comments/3179859967/replies`

## Phase 3: Verification
### 1. Run Tests
- **Action:** `go test -v ./test/e2e/provisioning/...` (using tags if necessary). Note: Full E2E requires environment setup, so I'll focus on compilation and unit-level sanity check if possible.
