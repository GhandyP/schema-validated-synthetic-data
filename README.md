# schema-validated-synthetic-data

Generate synthetic records that provably satisfy a declared schema. The model compiles a natural-language request
into a typed specification, deterministic Go builds the records from a seed, and a pure-Go validator re-checks
every declared rule before anything is printed. Dry run only: nothing is written to disk and no real data source is
read.

Go + Google ADK (Go v2).

## The two guarantees

**The model never writes a record.** It has exactly one job: turn prose into a `DatasetSpec` under a structured
output schema. The values come from `internal/generate`, which is seeded arithmetic, so a model answer cannot
produce an age outside its range, a repeated identifier, or a foreign key that points nowhere — not because the
model is careful, but because it is not in the value path. The compiled specification is then treated as untrusted
input and passes the same static rules as a hand-written file.

**Every declared rule is re-derived before printing.** `internal/validate` shares no code with the generator. It
reads the specification again and asks independently whether each rule holds: value type, range, closed domain,
pattern shape, uniqueness, nullability, reference resolution, record count, field order, and whether the payload
labels itself synthetic. A failing check is printed with the rule, the subject, and what was counted, and the batch
is withheld — the command exits `5`, which reports a defect in this repository rather than a mistake by the caller.

The result is a batch you can regenerate byte for byte, or refuse with a reason you can act on. There is no third
outcome that looks plausible.

## Quick start

Requirements: Go 1.26.5 or newer. A `GOOGLE_API_KEY` from [Google AI Studio](https://aistudio.google.com/apikey) is
needed only for `-doc`; the `-spec` path never calls a model.

```bash
# Generate, validate, and print a batch from a specification file.
go run . -spec testdata/support-tickets.json

# The same batch as JSON, including the specification, the seed, and the full report.
go run . -spec testdata/support-tickets.json -json

# A different seed: a different batch, with the same guarantees.
go run . -spec testdata/support-tickets.json -seed 42

# A specification the contract refuses, and every reason why.
go run . -spec testdata/impossible/reference-cycle.json

# Compile a plain-language request first, then run the pipeline.
export GOOGLE_API_KEY="your-key"
go run . -doc "Six fictional customers with a tier, an age, a signup date and a satisfaction
score, and twelve support tickets linked to those customers."
```

Set `ADK_MODEL` to use another Gemini model; the default is `gemini-flash-latest`.

## Determinism

The same specification and the same seed produce the same bytes. A value is a pure function of the seed and the
identity of the field it belongs to, so it cannot depend on how many values were drawn before it. The seed is
recorded in the JSON output and printed in the preview, and no timestamp is written anywhere, which is what makes
two runs comparable with `diff`:

```bash
go run . -spec testdata/support-tickets.json -json > first.json
go run . -spec testdata/support-tickets.json -json > second.json
diff first.json second.json
```

## Specification reference

A specification is JSON. `seed` is the only optional key at the top level; unknown keys are refused rather than
ignored, so a typo cannot quietly change what is generated.

| Field type | Required keys | Notes |
| --- | --- | --- |
| `string` | `values` or `pattern` | `pattern` tokens: `{seq}` for the record number, `{seq:W}` for a zero-padded width, `{word}` from a built-in 32-word list. |
| `integer` | `min`, `max` | Whole numbers only. |
| `number` | `min`, `max` | Two decimals, clamped to the declared range. |
| `boolean` | – | |
| `date` | `min_date`, `max_date` | `YYYY-MM-DD` only. |
| `enum` | `values` | A non-empty list of distinct strings. |
| `reference` | `references` | `"entity.field"`, pointing at a unique field. Reference cycles are refused. |

Optional keys: `unique`, `nullable`, `sensitive`. `unique` is accepted only where distinct values are provable —
an integer range, a date range, an enum or value list, a pattern containing `{seq}`, or a reference to a unique
field whose entity has enough records. It is never accepted for `number` or `boolean`, because a claim the
generator cannot prove is a claim the validator cannot check. `nullable` marks a column where one record in four is
`null`, and nulls are exempt from range, uniqueness, and reference rules. `sensitive` marks a column resembling
personal data: it changes nothing about generation, and it appears in the assumptions, because a column that
*looks* personal is exactly the one a reader must be able to see was synthesised.

`assumptions` is where a request records what it could not express. The generator adds its own entries for
nullability, sensitive columns, and templated wording, so the printed batch states what it is not claiming.

## CLI reference

| Flag | Default | Meaning |
| --- | --- | --- |
| `-spec` | – | Path to a specification file. Runs the whole pipeline without a model. |
| `-doc` | – | Plain-language request compiled by the model before generating. |
| `-seed` | the spec seed | Overrides the seed declared in the specification. |
| `-json` | `false` | Prints the dataset as JSON instead of the preview. |

| Exit code | Meaning |
| --- | --- |
| `0` | The batch was generated and every declared rule held. |
| `2` | The command line was wrong. |
| `3` | The specification, written or compiled, was refused. |
| `4` | The model could not be used. |
| `5` | The validator found a rule the generator broke: a defect in this repository. |

The preview prints at most 20 records per entity; `-json` carries the whole batch.

## Package map

| Path | Responsibility |
| --- | --- |
| `main.go` | Signal-aware entry point; delegates to the CLI. |
| `internal/spec` | The contract: types, strict JSON parsing, and every static rule. No I/O. |
| `internal/generate` | The deterministic generator: seeded domains, uniqueness, relationships. |
| `internal/validate` | Independent re-checks of a generated batch. Shares no code with the generator. |
| `internal/agent` | The only model call: compiling a request into a `DatasetSpec` with ADK and Gemini. |
| `internal/cli` | Flags, the dry-run pipeline, the preview, the JSON output, and the exit codes. |
| `testdata/` | A runnable specification, and three that the contract refuses. |
| `odd/tasks/` | The specification and task log this repository was built from, and the case plan. |

## What this does not do

- **The wording is templated.** `{word}` draws from a 32-word list in `internal/generate/words.go`. A model in the
  value path would make the output read better and would break the determinism guarantee, so it is not there.
- **Validation proves the spec, not the intent.** It can prove that every record obeys the declared types, ranges,
  uniqueness, and references. It cannot prove that the specification describes the data you had in mind.
- **There is no export.** Nothing is written, and there is no loader. Loading synthetic records into a test system
  is a separate decision with its own approval, and this repository deliberately has no code path for it.
- **Scalars only.** A field holds one value. There are no nested objects, no arrays as columns, and no per-row
  correlation beyond declared references.
- **`number` fields carry two decimals**, so a range narrower than `0.01` collapses to its bounds.

## Safety

No real data source is read. Values are generated from the specification and the seed, including the columns marked
`sensitive`; nothing is sampled, inferred, or copied from a real record. The prompt sent to the model is the request
you wrote, and the specification it answers with, and nothing else. The output is labelled `"synthetic": true` in
JSON and carries the seed, so a batch cannot be mistaken for an extract.

## License

MIT. See [LICENSE](LICENSE).
