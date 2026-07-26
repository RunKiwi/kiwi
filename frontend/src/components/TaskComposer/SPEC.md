# TaskComposer — mention-aware task box

Replaces the plain `<textarea>` in `app/(dashboard)/page.tsx` with a rich editor where
`@` / `#` / `/` insert typed **chips** that serialize straight into `PlanRequest`.
Live prototype (interaction + 3 chip styles): see the design artifact linked in the PR.

## Why Tiptap (not hand-rolled)
`contentEditable` caret/IME/paste/undo handling is where DIY mention editors rot.
Tiptap (ProseMirror) gives each mention a real schema node, so serialization is
"walk the doc", not "re-parse English". v3 works on React 19 / Next 16.

## Triggers → one Mention extension **each** (you can't share one for 3 chars)
| Trigger | `name` | Inserts | Data source |
|---|---|---|---|
| `@` | `refMention` | file · symbol · branch (repo-gated) | repo tree · symbol index · branches |
| `#` | `workMention` | task (org-wide) · GitHub issue | `SearchJobLearnings` (org-scoped) · issues API |
| `/` | `actionMention` | test · post · notify · model · workers | Slack channels · members · model list |

## Payload mapping — **maps today vs needs backend**
Serialize the doc to `Partial<PlanRequest>`. Text nodes → `task` string (keep chip
markers inline so the planner sees them); mention nodes → fields:

| Chip | Field | Status |
|---|---|---|
| `@file` | `files[]` | ✅ exists |
| `@branch` | `ref` | ✅ exists |
| `@symbol` | *(stays inline in `task`)* | ✅ no field needed |
| `#task` **Reference** | `reference_job_ids[]` + `reference_mode:"manual"` | ✅ exists |
| `#task` Continue / After / Avoid | new `reference_tasks:[{id,mode}]` | ⚠️ backend TODO |
| `#issue` | *(inline in `task`)* now; later `issues[]` | ⚠️ inline for v1 |
| `/test` | `test_cmd` | ✅ exists |
| `/model` | `model` | ✅ exists |
| `/workers` | `max_workers` | ✅ exists |
| `/post` (Slack) | new `slack_post[]` | ⚠️ backend TODO |
| `/notify` (reviewer) | new `reviewers[]` | ⚠️ backend TODO |

**v1 ships the ✅ rows only.** ⚠️ rows render as chips but no-op (or fold into `task`
text) until the backend lands — call that out in the PR so nobody assumes they're wired.

## Files
```
components/TaskComposer/
  TaskComposer.tsx     "use client" — owns useEditor, emits onChange(Partial<PlanRequest>)
  extensions/          refMention.ts · workMention.ts · actionMention.ts (Mention + suggestion)
  SuggestList.tsx      the sectioned dropdown (ReactRenderer target)
  TaskNodeView.tsx     ReactNodeViewRenderer chip: status dot + Ref/Continue/After/Avoid popover
  suggest.ts           async providers hitting existing endpoints
  serialize.ts         doc JSON → Partial<PlanRequest>
```

## Deps
`@tiptap/react @tiptap/pm @tiptap/extension-mention @tiptap/suggestion`
plus `Document Paragraph Text` (skip StarterKit — we don't want headings/lists).
Dropdown positioning: `@floating-ui/dom` (don't pull in tippy).

## Gotchas (read before coding)
- **SSR**: `useEditor({ immediatelyRender: false, ... })` or Next hydration errors. Client component only.
- Custom mention node needs `addAttributes()` for `kind/value/label/status/mode` + `renderHTML`/`parseHTML` so it round-trips.
- Task chip = `ReactNodeViewRenderer` (needs the mode popover); others can be plain node views.
- `@` gate: when no repo selected, suggestion list returns a single disabled "Select a repo first" row.
- Chip CSS reuses the chosen concept (default: Concept A soft pill) — green `@`, amber `#`, zinc `/`.
- **Repo rule**: read `node_modules/next/dist/docs/` + `frontend/AGENTS.md` before writing components (this Next is modified). Verify the installed Tiptap version is React-19 compatible.

## Advanced-panel collapse
When a field is set via a chip (`files`, `ref`, `test_cmd`, `model`, `max_workers`,
reference jobs), grey out / hide that Advanced input with a "set inline ✓" note.
Advanced stays as the override/escape hatch.
