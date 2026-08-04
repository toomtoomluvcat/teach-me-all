# Handoff — exam-quality prototype

Updated 2026-08-05. This is the short operational handoff. Historical
measurements and reasoning belong in `backend/prototype-exam-quality/VERDICT.md`;
do not copy them back here.

## Current state

- Repo: `E:\contribute\teach-me-all`
- Branch: `prototype/exam-quality`
- HEAD: `f03d7cf` (`feat: Implement question planning and evidence compilation features`)
- The working tree is intentionally dirty from the folder refactor. Do not
  reset or restore the deleted old paths. The new layout is:

  ```text
  backend/prototype-exam-quality/
  ├─ cmd/protoexam/main.go       process entrypoint only
  ├─ app/                        CLI orchestration, TUI, cache, artifacts, benchmark
  ├─ examgen/                    model, evidence, gates, generation
  ├─ llm/                        core, providers, generation, judging
  └─ pdfx/                       extract, bundle
  ```

- The refactor was checked with `go test -count=1 ./...`, `go vet ./...`,
  `go build ./...`, and `git diff --check`.
- `CODEBASE-TOUR.html` is the visual codebase map. `README.md` is the runbook.

## What the prototype does

```text
PDF → Docling pages → page-bounded chunks → pass-1 outline/evidence graph
   → optional graph compile + bounded two-hop context + coverage contract
   → pass-2 question generation → deterministic QC → JSON artifact
```

The prototype measures whether questions are readable and tied to a specific
source, then compares them with NotebookLM. It is not production architecture.

Current generation modes:

- Normal per-chunk generation: default.
- `--question-plan`: lesson-level coverage slots.
- `--graph-compile`: atomic source claims with provenance.
- `--set-generation`: graph atoms, bounded cross-lesson context, contract,
  deterministic contract preflight, and multiple set candidates.
- `--calc-tool`: calculator tool turn before numeric generation; Go always
  re-checks the stored expression afterward.
- Set generation now also runs one batch semantic-quality review per candidate
  that has QC-accepted questions. It is advisory only and is a tie-break after
  deterministic acceptance; malformed or incomplete reviews are ignored.
- Set contracts now choose atoms compatible with an explicit skill target before
  slot creation. Explicit difficulty targets are enforced; unspecified
  calculation difficulty remains open. Missing provenance IDs may be recovered
  only from one exact quote-to-atom-to-slot match; ambiguous matches still fail.

## Acceptance boundary

The production path is deliberately QC-only; LLM judges are advisory and
`AddJudgeGates` is currently a compatibility no-op. Hard acceptance is based
on deterministic checks:

- structural/well-formed question and exactly one correct choice;
- usable source role, not a pre-learning/answer-key section;
- exact source quote present in the cited chunk;
- arithmetic expression agrees with the keyed answer;
- declared physical unit appears in the keyed answer;
- no duplicate against accepted questions;
- set-generation additionally checks coverage-slot and evidence-atom usage.

The blind/source semantic judges and `SourceDependency` types remain available
for experiments. Do not make them hard gates again without a calibrated eval:
the model already knows upper-secondary subject matter, so “the model can
answer without the passage” is not the same as “a learner does not need the
passage.”

## Latest evidence

Latest physics artifact is
`.scratch/e0312e3e107a1953/benchmark-all.json` for `openstax-physics.pdf`,
pages `140-220`, using DeepSeek, graph compile, set generation, and 3
candidate sets:

- The false-negative audit found 3 questions with valid content but the wrong
  atom/slot provenance, plus 1 conceptual question mislabeled as calculation.
  The gate was correct for those metadata contracts; the contract selection and
  provider-provenance path were the defects.
- The latest combined run after the writer/retry fix reached 5/5 on
  application-easy, application-hard, and calculation. The selected sets had
  no remaining coverage/quote failures; all target questions were accepted.
  Four of the nine base candidate calls needed the bounded one-shot repair,
  and the repair was capped at one call per candidate. The earlier combined
  hard/calculation 0/5 result was therefore a writer-adherence/provider-variance
  problem, not a reason to weaken the gate.

The current set-writer fix has three parts: dynamic finite ID vocabularies in
the set schema where the provider supports them, an explicit slot execution
protocol in the system/user prompts, and a retry request containing only
unaccepted slots plus compact failure memory. A provenance recovery bug was
also fixed: the repaired question is now written back before the cheap gates
run.

The first uncached graph compile made 53 provider calls in 9m41s (162,010
input + 104,678 output tokens). The latest warm benchmark made 50 calls in
2m47s: generate-set 13 calls / 188,830 input + 15,096 output tokens,
calc-tool 28 / 43,737 + 1,912, and quality/set 9 / 19,961 + 3,524. Total:
252,528 input + 20,532 output = 273,060 provider tokens. The extra cost is the
four bounded repairs; without them this run would have been 46 calls.
- `.scratch/7cbf67a2421732f6/`: two circular-motion lesson runs, each 8/8
  accepted by deterministic QC. The DeepSeek run used 15 provider calls and
  reported 42,723 input + 14,549 output tokens (57,272 total) in about 1m17s.

Known quality caveat: passing QC can still hide recall-heavy, near-duplicate,
or pedagogically weak questions. The semantic grader currently over-trusts the
claimed key and is not calibrated enough to replace reading the outputs.
The old blind human-label baseline is noisy: ours 9/15 good, NotebookLM-pages
11/15, NotebookLM-book 7/15; treat those numbers as approximate, not truth.

## Next actions

1. Calibrate the semantic reviewer: make it independently choose the best
   option and explicitly penalize key/source contradictions and overclaimed
   application difficulty.
2. Run the same physics source through NotebookLM and compare content quality,
   not just pass rate. Keep provider, pages, budget, and prompt fixed.
3. Report quote/coverage failures separately from semantic failures; do not
   turn the semantic reviewer into a hard gate without a calibrated eval.
4. Only after that decide whether prompts, tools, or generation modes need more
   changes. Avoid adding another gate merely to raise the acceptance percentage.
5. If the prototype result is good enough, lift only `examgen/` pieces
   into production and design the storage migration separately.

## Commands

From `backend/prototype-exam-quality`:

```powershell
go test -count=1 ./...
go vet ./...
go build ./...
go run ./cmd/protoexam --provider deepseek --pdf ../../samples/openstax-physics.pdf --pages 140-220 --benchmark all --budget 5
```

Use `--fresh` after changing extraction, pass-1 prompts, or graph compilation.
DeepSeek has no embedding endpoint in this prototype, so duplicate/ranking
embeddings are disabled unless a separate embedder is configured. Docling is
the only supported extraction path; inspect `--extract-only` output before
blaming generation.
