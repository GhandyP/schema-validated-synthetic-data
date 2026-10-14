# schema-validated-synthetic-data

Public portfolio repository built from scratch: the model compiles a natural-language request into a typed
`DatasetSpec`, deterministic Go generates the records from a seed, and a pure-Go validator re-checks every rule.
Dry run only: nothing is written to disk, no real data source is read.

Source plan: the case README that shipped in this directory (`README.md`, quoted in `S#`), which is the cookbook
case `case_32_synthetic_data_generation` prototype.

## Specs

**S1 — Public standalone portfolio repository.**
Name `schema-validated-synthetic-data`, public, owned by `GhandyP`. Go module
`github.com/GhandyP/schema-validated-synthetic-data`. LICENSE: `MIT License` / `Copyright (c) 2026 GhandyP`,
matching the sibling published repositories.

**S2 — From scratch, self-contained.**
The prototype `main.go` imports `"github.com/GhandyP/adk-go-enterprise-cookbook/internal/adkhelper"`, which does
not exist on GitHub (`gh repo view GhandyP/adk-go-enterprise-cookbook` -> `Could not resolve to a Repository`).
That import is removed; the repository wires Google ADK itself, like `02-professional-email-drafting`.
Model: `google.golang.org/adk/v2 v2.2.0`, Gemini, default `gemini-flash-latest`, `ADK_MODEL` overrides it,
`GOOGLE_API_KEY` supplies the key.

**S3 — ODD tracking.**
This document plus an Engram mirror and a visible task list, created before the first source write. One work-unit
commit per task, tests and docs alongside the behavior, Conventional Commit messages.

**S4 — Contract.**
> **Contract:** Define `DatasetSpec{fields[], relationships[], count, constraints}` -> `SyntheticDataset{records[], assumptions[], seed, validation}`.

**S5 — Go generates; the model only compiles the spec.**
> Go genera; el modelo solo compila el spec

Exact determinism is the guarantee: same spec + same seed -> identical bytes. The model never authors a record,
so it cannot violate a range, a uniqueness rule, or a foreign key.

**S6 — ADK layer.**
> **ADK layer:** Add a record schema and generation session; keep generation bounded, and use no function tool until export is requested.

The ADK agent's only job is `docs -> DatasetSpec` under a structured output schema. No function tool is used.

**S7 — Domain boundary.**
> **Domain boundary:** Add schema parser, deterministic seed/uniqueness validator, relationship generator, and file-export adapter.

v1 implements the schema parser, the deterministic seeded generator, the uniqueness/range/type validator, and the
relationship (foreign key) generator. The file-export adapter is deferred, per S11.

**S8 — Fixtures and validation.**
> **Fixtures and validation:** Cover enums, numeric/date ranges, foreign keys, nullability, PII-like fields, and impossible constraints with repeatable seeds.

**S9 — Safety and labeling.**
> **Safety:** Never use real personal records as prompt fixtures; label output synthetic and require approval before sharing or loading it into a test system.
> This is intentionally text-only: it does not read production data, anonymize a source database, or load records anywhere.

The preview and the JSON output are labeled synthetic and print the seed. PII-like fields are generated from the
spec, never sampled from a real record.

**S10 — Done when.**
> - [ ] Every record passes the declared type, range, uniqueness, and relationship checks.
> - [ ] Seed and assumptions make the batch reproducible and clearly synthetic.
> - [ ] Fixtures cover invalid and impossible specifications.
> - [ ] Export or loading requires approval and never uses unapproved real data.

The fourth item is satisfied structurally in v1: there is no export or load path at all, so there is nothing to
approve. It becomes an acceptance criterion for the deferred export adapter (S11).

**S11 — Decisions taken with the owner (v1 scope).**
> Dry-run en memoria, sin export
> Mínimo: código + README + LICENSE

No export adapter, no CI workflow, no Dockerfile, no Makefile, no CHANGELOG. Verification runs locally:
`gofmt -l .` clean, `go vet ./...`, `go test -race -count=1 ./...`.

**S12 — Key-free path.**
`-spec <file>` runs the whole pipeline without a model or an API key. `-doc <text>` is the only path that calls
Gemini. Tests never require `GOOGLE_API_KEY`.

**S13 — Honest limits stated in the README.**
The repository must state what it does not prove: the wording of free-text fields comes from a built-in synthetic
word list, not from a model, so it reads as templated; and validation proves that the records obey the declared
spec, not that the spec describes the world the reader has in mind.

## Tasks

| ID | Task | Specs | Route | Commit |
| --- | --- | --- | --- | --- |
| T0 | Scaffold: `go.mod`, `LICENSE`, `.gitignore`, `main.go` stub, create and push the public repository | S1, S2, S3, S11 | inline | `chore: scaffold the module, license, and repository metadata` |
| T1 | `internal/spec`: dataset contract, JSON parsing, static validation that refuses impossible specs | S4, S8, S10 | inline | `feat(spec): define the dataset contract and refuse impossible specs` |
| T2 | `internal/generate`: seeded deterministic generation of records, uniqueness, ranges, relationships | S5, S7, S8 | inline | `feat(generate): add deterministically seeded record generation` |
| T3 | `internal/validate`: independent re-check of every declared rule over generated records | S7, S10 | inline | `feat(validate): verify every declared rule independently` |
| T4 | `internal/agent`: ADK wiring that compiles a natural-language request into a `DatasetSpec` | S5, S6 | inline | `feat(agent): compile a natural-language request into a typed spec` |
| T5 | `internal/cli`: flags, dry-run pipeline, preview and JSON output, exit codes | S9, S12 | inline | `feat(cli): wire the dry-run pipeline with preview and JSON output` |
| T6 | README in the portfolio style + fixture pinning test | S1, S9, S13 | inline | `docs: document the guarantees, quick start, and limits` |

Route is `inline` for every task. Reviewer workload and dependency, not size, decide it: the tasks are strictly
sequential (contract -> generator -> validator -> CLI), and the design is fully resolved in the parent session, so
handing it to parallel writers would add transfer cost without parallelism. Branch `feat/seeded-generation-pipeline`
carries T1-T6; T0 is the initial commit on `main` because it creates the repository.

## Log

- **L1 (original request, verbatim):**
  > explora este proyecto , crea un repo publico que la idea es hacerlo desde el inicio pero para que sea un
  > repo dentro de mi portafolio en github , y luego continuar con crear en proyecto con odd , implementa el
  > plan con go google adk
- **L2 (owner decisions, verbatim):**
  1. `schema-validated-synthetic-data`
  2. `Go genera; el modelo solo compila el spec`
  3. `Dry-run en memoria, sin export`
  4. `Mínimo: código + README + LICENSE`
- **L3 (evidence — hazard found during exploration):** `git rev-parse --show-toplevel` from this directory
  returned `/home/ghandy`: the whole home directory is a git repository with no commits and three already-staged
  files (`Desktop/informe mami/...`, `odd/tasks/inidi-september-2026-report.md`). Every git command in this feature
  must run against a fresh repository initialized inside this directory, and the top level must be verified before
  each commit. The home repository is never touched.
- **L4 (evidence — input is not buildable):** `main.go` imports
  `github.com/GhandyP/adk-go-enterprise-cookbook/internal/adkhelper`; `gh repo view
  GhandyP/adk-go-enterprise-cookbook` fails with `Could not resolve to a Repository with the name
  'GhandyP/adk-go-enterprise-cookbook'`, and no local copy exists. S2 is therefore forced, not a preference.
- **L5 (evidence — reference wiring):** `02-professional-email-drafting` supplies the proven ADK v2.2.0 pattern
  used by T4: `llmagent.New(llmagent.Config{..., OutputSchema})`, `runner.NewInMemory`, `runner.Run` returning
  `iter.Seq2[*session.Event, error]`, `gemini.NewModel(ctx, name, &genai.ClientConfig{APIKey: key})`, and a fake
  `model.LLM` for tests.
- **L6 (evidence — toolchain):** local `go1.26.4` with `GOTOOLCHAIN=auto`; the sibling modules declare `go 1.26.5`,
  so the toolchain is downloaded on demand. `google.golang.org/adk/v2@v2.2.0` is present in the module cache.
