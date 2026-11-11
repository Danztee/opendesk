# FreeScout import implementation plan

> **For agentic workers:** Use subagent-driven-development to implement these tasks.

**Goal:** Resolve #396 by importing FreeScout conversations, replies, internal notes and attachments.

**Architecture:** Add a read-only `source.Source` adapter using FreeScout's API & Webhooks module and the existing background import engine. Require the instance URL and API key, authenticate through the header, map mailboxes to groups and users to agents. Do not import knowledge-base articles, which this API does not expose. Existing check, mapping, resume, sync, undo and guarded download behavior apply.

**Tech stack:** Go, PostgreSQL, React/TypeScript, existing import wizard.

**Spec:** Issue #396 and contracts/imports.md; https://api-docs.freescout.net/.

## Global constraints

- Existing workspace permissions, encrypted credentials, safe network clients and domain writes apply.
- HTTPS instance URL; HTTP/private destinations allowed only in development.
- API keys never appear in query strings, logs, or error messages.
- All conversation statuses must be explicitly requested; the API defaults to active only.
- Published customer messages, agent replies and notes are imported oldest first. Drafts and system events are omitted.
- Files use existing attachment validation and download limits. No provider credentials sent to other origins.

## Review focus

Pagination and nested threads; all statuses; customer/agent attribution; subdirectory installations; SSRF and credential leakage.

## Tasks

- [ ] Add source adapter and HTTP fixture tests for authentication, pagination, status mapping, notes, attachments, filtering, errors and hostile links.
- [ ] Register source in Go/API/schema; add migration above main and integration coverage.
- [ ] Add wizard source, credentials, mock and frontend tests; update contracts and user guide.
- [ ] Independent review, targeted tests, screenshots where useful, committed-HEAD QA, push and open PR.
