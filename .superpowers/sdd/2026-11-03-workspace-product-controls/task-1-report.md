# Task 1 report: Persist and expose workspace product settings

## What changed

- Added `tickets_enabled` and `mail_enabled` to the existing workspace settings model and patch validator. Missing values deserialize as enabled, preserving the behavior of old workspace rows without a migration. Newly created workspaces explicitly store both as enabled.
- Added the effective Mail fields `mail_available` and `mail_disabled_reason` to the Workspace response. The reason is selected in precedence order: instance Collab disabled, missing workspace Collab entitlement, then the workspace Mail setting disabled.
- Kept the existing PATCH flow and added a workspace row lock around the merged product state check. It rejects a resulting state with both products off as `422 at_least_one_product_required`, so concurrent writes cannot bypass the invariant.
- Product flag changes use the existing JSONB merge, audit `workspace.updated`, and `workspace.updated` event publication in the same transaction.
- Updated `contracts/api.md`, the web API types, and the workspace settings unit test.

## Tests and verification

- `go test ./internal/core` — passed (`ok`, 1.092s).
- `go build ./...` — passed.
- `go vet ./...` — passed.

## Files changed

- `internal/core/workspaces.go`
- `internal/core/workspace_settings_test.go`
- `contracts/api.md`
- `web/src/lib/api/types.ts`

## Self-review

- Confirmed existing rows with no product keys map to both enabled in the compatibility helper and response model.
- Confirmed product validation reads and checks the merged settings while holding the workspace row lock.
- No data migration is required. No unresolved concerns.
