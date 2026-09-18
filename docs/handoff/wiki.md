# Wiki-ready documentation

The handoff pages use portable Markdown tables, relative links and fenced Mermaid diagrams. They can be read in the repository immediately. The exporter creates flat GitHub Wiki pages, a `Home.md`, and `_Sidebar.md`, rewrites inter-page links to wiki page names, and rewrites source links to the repository. No publication, credential access, or git push is performed.

From the repository root in PowerShell 7:

```powershell
./tools/export-handoff-wiki.ps1 -OutputDirectory "$env:TEMP/mockagents-wiki-export" -SourceRevision main
```

Choose an empty output directory; existing nonempty directories are rejected. For a long-lived published wiki, first merge the documentation changes, then pass that commit SHA as `-SourceRevision` so new tooling/source links resolve to the reviewed revision. `main` links follow later repository changes. This export command copies the handoff and its dated review pages, including reference catalogs; it does not copy arbitrary private workspace files.

Copy the resulting `.md` pages into the repository's wiki checkout or paste individual pages into the wiki editor. Mermaid rendering depends on the destination; the text diagrams remain readable as source on renderers without Mermaid support. Re-export from the canonical repository documents after changes rather than editing two independent copies.

The proposed repository structure is already installed under [docs/README.md](../README.md): `handoff/` for guides, `handoff/reference/` for generated catalogs, and existing `reviews/` for evidence. Existing user guides, design material, epics and release instructions retain their locations.
