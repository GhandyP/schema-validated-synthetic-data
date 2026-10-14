# schema-validated-synthetic-data

Generate synthetic records that provably satisfy a declared schema: the model compiles a natural-language request
into a typed `DatasetSpec`, deterministic Go builds the records from a seed, and a pure-Go validator re-checks every
declared rule. Dry run only — nothing is written to disk and no real data source is read.

Go + Google ADK (Go v2).

**Status: under construction.** The tasks, their acceptance criteria, and the evidence trail live in
[`odd/tasks/schema-validated-synthetic-data.md`](odd/tasks/schema-validated-synthetic-data.md); the case plan this
repository implements is preserved verbatim in
[`odd/tasks/case-32-source-plan.md`](odd/tasks/case-32-source-plan.md).

## License

MIT. See [LICENSE](LICENSE).
