# 32 — Synthetic data generation

## Goal

Propose fictional records that match a supplied schema and constraints, while making clear that no real data source or database is used.

## Run the current prototype

From this case directory:

```bash
export GOOGLE_API_KEY="your-key"
go run .
```

`internal/adkhelper.Run` uses ADK Go v2.2.0, Gemini, and `gemini-flash-latest`; set `ADK_MODEL` to override the model.

Sample prompt:

```text
Generate three fictional records for this schema and list validation assumptions.
```

## What exists now

- `main.go (exists)` gives a schema description and constraints to one focused Gemini ADK agent.
- The response is free-form text; record IDs, field types, ranges, relationships, uniqueness, and provenance are not enforced.
- This is intentionally text-only: it does not read production data, anonymize a source database, or load records anywhere.

## Missing elements

- **Contract:** Define `DatasetSpec{fields[], relationships[], count, constraints}` -> `SyntheticDataset{records[], assumptions[], seed, validation}`.
- **ADK layer:** Add a record schema and generation session; keep generation bounded, and use no function tool until export is requested.
- **Domain boundary:** Add schema parser, deterministic seed/uniqueness validator, relationship generator, and file-export adapter.
- **Fixtures and validation:** Cover enums, numeric/date ranges, foreign keys, nullability, PII-like fields, and impossible constraints with repeatable seeds.
- **Safety:** Never use real personal records as prompt fixtures; label output synthetic and require approval before sharing or loading it into a test system.

## Target structure

```text
32-synthetic-data-generation/
├── main.go (exists)
├── README.md (exists)
├── schema.go (next)
├── generator.go (next)
├── validator.go (next)
├── testdata/spec.json (next)
└── main_test.go (next)
```

## Implementation order

1. Define field, relationship, constraint, record, and provenance schemas.
2. Implement deterministic validation and seeded generation for in-memory records.
3. Add session limits and explicit assumptions for unsupported constraints.
4. Test ranges, uniqueness, nullability, relationships, and impossible specs.
5. Add reviewed export only after a synthetic-data owner confirms the fixture.

## Done when

- [ ] Every record passes the declared type, range, uniqueness, and relationship checks.
- [ ] Seed and assumptions make the batch reproducible and clearly synthetic.
- [ ] Fixtures cover invalid and impossible specifications.
- [ ] Export or loading requires approval and never uses unapproved real data.

## Next improvement

Build a seeded validator around `DatasetSpec` before asking the model to generate more records.
