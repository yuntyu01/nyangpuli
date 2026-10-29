# Arcanite source snapshot

- Source: https://github.com/katelouie/arcanite
- Commit: `c025b97c49e564e16019cafba381375fd1697a17`
- License: MIT, copyright (c) 2026 Kate Louie. Full notice in `LICENSE`.
- Imported byte-for-byte: 78 files from `src/arcanite/cards/json/tarot/`, `src/arcanite/spreads/tarot-spreads.json`, and `LICENSE`.
- `upstream-files.json` records original Git blob SHA, local path, size, and SHA-256 for all 80 imported files. All 80 original Git blob hashes were verified at import.
- Local addition: `data.go` embeds text data. No card images or Python runtime are included.

The Go adaptation in `internal/tarot/engine.go` follows the position / orientation / question-context / card-relationship assembly idea. It is not a full Arcanite port and does not invoke its Python engine, prompts, LLM synthesizer, or drawing algorithm.

Changes from the inspected Python assembly:

1. Fixed cards are supplied by the caller; no hidden re-draw.
2. `question_contexts.*` spread mappings are resolved from the actual question-context object, with explicitly reported core fallback for unavailable paths.
3. Directional A→B and B→A texts remain distinct. One-way links are not discarded based on alphabetical card-ID order.
4. Empty/placeholder relationships are excluded and reported. Unknown target IDs are counted and not silently normalized.
5. Outputs are evidence and prompts, with explicit limits on using symbolic prose as factual evidence.

Source audit: 78 cards, 11 spreads, 1,379 relationship entries, 676 placeholder/empty relationship entries, and 23 relationship entries targeting absent card IDs. These two defect counts may overlap. They are mechanical completeness checks, not expert quality scores. No prose was edited to hide these findings.
