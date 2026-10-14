package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GhandyP/schema-validated-synthetic-data/internal/agent"
	"github.com/GhandyP/schema-validated-synthetic-data/internal/spec"
)

// fixturePath names a file under the repository's testdata directory. Packages
// under internal/ run with their own directory as the working one, so the path
// walks up to the root instead of duplicating the fixtures beside the test.
func fixturePath(name string) string {
	return filepath.Join("..", "..", "testdata", name)
}

func runCLI(t *testing.T, deps Deps, args ...string) (int, string, string) {
	t.Helper()

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), args, deps, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func decodeDataset(t *testing.T, output string) spec.SyntheticDataset {
	t.Helper()

	var dataset spec.SyntheticDataset
	if err := json.Unmarshal([]byte(output), &dataset); err != nil {
		t.Fatalf("decode dataset: %v\noutput:\n%s", err, output)
	}
	return dataset
}

func TestSpecRunNeedsNoModelAndPrintsThePreview(t *testing.T) {
	deps := Deps{NewCompiler: func(context.Context) (agent.Compiler, error) {
		t.Error("a -spec run built a compiler, want none: the pipeline does not need a model")
		return nil, errors.New("unreachable")
	}}

	code, stdout, stderr := runCLI(t, deps, "-spec", fixturePath("support-tickets.json"))

	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, ExitOK, stderr)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want nothing", stderr)
	}

	for _, want := range []string{
		"batch: support_tickets",
		"seed:  20260920",
		"customer (6 records)",
		"ticket (12 records)",
		"CUS-0001",
		"user0001@example.test",
		"validation: 41 of 41 declared rules hold",
		"the request did not state how many customers exist, so six were generated",
		"synthetic data: every value above was generated from the specification and the seed.",
		"No real data source was read, and nothing was written to disk.",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout does not contain %q", want)
		}
	}

	position := -1
	for _, field := range []string{"id", "tier", "age", "signed_up", "active", "satisfaction", "email"} {
		at := strings.Index(stdout, field)
		if at <= position {
			t.Fatalf("column %q does not appear after the previous one", field)
		}
		position = at
	}
}

func TestJSONOutputIsStableAndCarriesTheWholeBatch(t *testing.T) {
	_, first, _ := runCLI(t, Deps{}, "-spec", fixturePath("support-tickets.json"), "-json")
	_, second, _ := runCLI(t, Deps{}, "-spec", fixturePath("support-tickets.json"), "-json")

	if first != second {
		t.Fatalf("two runs of the same spec differ:\nfirst  %.300s\nsecond %.300s", first, second)
	}

	dataset := decodeDataset(t, first)
	if !dataset.Synthetic {
		t.Error("dataset.Synthetic = false, want the payload to label itself synthetic")
	}
	if dataset.Seed != 20260920 {
		t.Errorf("dataset.Seed = %d, want the declared seed", dataset.Seed)
	}
	if len(dataset.Entities) != 2 {
		t.Fatalf("dataset holds %d entities, want 2", len(dataset.Entities))
	}
	if got := len(dataset.Entities[0].Records); got != 6 {
		t.Errorf("customer has %d records, want 6", got)
	}
	if got := len(dataset.Entities[1].Records); got != 12 {
		t.Errorf("ticket has %d records, want 12", got)
	}
	if dataset.Validation == nil || !dataset.Validation.Passed {
		t.Fatalf("dataset.Validation = %+v, want a passing report", dataset.Validation)
	}
	if dataset.Validation.Total != 41 {
		t.Errorf("dataset.Validation.Total = %d, want 41 declared rules", dataset.Validation.Total)
	}
}

func TestSeedFlagOverridesTheDeclaredSeed(t *testing.T) {
	_, declared, _ := runCLI(t, Deps{}, "-spec", fixturePath("support-tickets.json"), "-json")
	code, overridden, stderr := runCLI(t, Deps{}, "-spec", fixturePath("support-tickets.json"), "-json", "-seed", "42")

	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, ExitOK, stderr)
	}

	dataset := decodeDataset(t, overridden)
	if dataset.Seed != 42 {
		t.Errorf("dataset.Seed = %d, want the flag value 42", dataset.Seed)
	}
	if overridden == declared {
		t.Error("changing the seed did not change the batch")
	}
}

func TestDocumentRunCompilesWithTheInjectedCompiler(t *testing.T) {
	t.Setenv("ADK_MODEL", "gemini-test")

	compiled := spec.DatasetSpec{
		Name: "compiled_batch",
		Entities: []spec.Entity{{
			Name:  "row",
			Count: 2,
			Fields: []spec.Field{
				{Name: "id", Type: spec.TypeString, Unique: true, Pattern: "R-{seq}"},
			},
		}},
	}
	deps := Deps{NewCompiler: func(context.Context) (agent.Compiler, error) {
		return agent.Fake{Spec: compiled}, nil
	}}

	code, stdout, stderr := runCLI(t, deps, "-doc", "two fictional rows")

	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, ExitOK, stderr)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want nothing", stderr)
	}
	for _, want := range []string{
		"batch: compiled_batch",
		"row (2 records)",
		"the specification was compiled with gemini-test from a plain-language request",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout does not contain %q\noutput:\n%s", want, stdout)
		}
	}
}

func TestUsageErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "no input",
			args: nil,
			want: "give -spec to read a specification, or -doc to compile a request",
		},
		{
			name: "both inputs",
			args: []string{"-spec", fixturePath("support-tickets.json"), "-doc", "something"},
			want: "-spec and -doc are mutually exclusive",
		},
		{
			name: "a positional argument",
			args: []string{"-spec", fixturePath("support-tickets.json"), "extra"},
			want: `unexpected argument "extra"`,
		},
		{
			name: "an unknown flag",
			args: []string{"-nope"},
			want: "flag provided but not defined: -nope",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := runCLI(t, Deps{}, tt.args...)

			if code != ExitUsage {
				t.Fatalf("exit code = %d, want %d", code, ExitUsage)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want nothing", stdout)
			}
			if !strings.Contains(stderr, tt.want) {
				t.Errorf("stderr does not contain %q\nstderr:\n%s", tt.want, stderr)
			}
			if !strings.Contains(stderr, "usage: schema-validated-synthetic-data") {
				t.Errorf("stderr does not print the usage line\nstderr:\n%s", stderr)
			}
		})
	}
}

func TestHelpExitsWithoutError(t *testing.T) {
	code, stdout, stderr := runCLI(t, Deps{}, "-h")

	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d", code, ExitOK)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing", stdout)
	}
	if !strings.Contains(stderr, "usage: schema-validated-synthetic-data") {
		t.Errorf("stderr does not print the usage\nstderr:\n%s", stderr)
	}
}

func TestRefusedSpecificationListsEveryProblem(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "unique boolean",
			path: fixturePath(filepath.Join("impossible", "unique-boolean.json")),
			want: "the specification was refused:\n" +
				"  field \"flag.on\": unique is not supported for boolean fields; the domain holds two values\n",
		},
		{
			name: "a date range that cannot hold the batch",
			path: fixturePath(filepath.Join("impossible", "unique-date-range.json")),
			want: "the specification was refused:\n" +
				"  field \"visit.day\": unique needs 5 distinct values but 2026-01-01..2026-01-03 provides 3 days\n",
		},
		{
			name: "a reference cycle",
			path: fixturePath(filepath.Join("impossible", "reference-cycle.json")),
			want: "the specification was refused:\n" +
				"  entity \"node\": references create a cycle: node -> node\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := runCLI(t, Deps{}, "-spec", tt.path)

			if code != ExitRefused {
				t.Fatalf("exit code = %d, want %d", code, ExitRefused)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want nothing: a refused spec prints no records", stdout)
			}
			if stderr != tt.want {
				t.Errorf("stderr =\n%q\nwant\n%q", stderr, tt.want)
			}
		})
	}
}

func TestEveryProblemOfASpecIsReportedOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "two-problems.json")
	const twoProblems = `{
  "name": "broken",
  "seed": 1,
  "entities": [
    {
      "name": "row",
      "count": 4,
      "fields": [
        {"name": "on", "type": "boolean", "unique": true},
        {"name": "day", "type": "date", "unique": true, "min_date": "2026-01-01", "max_date": "2026-01-02"}
      ]
    }
  ]
}`
	if err := os.WriteFile(path, []byte(twoProblems), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	code, _, stderr := runCLI(t, Deps{}, "-spec", path)
	if code != ExitRefused {
		t.Fatalf("exit code = %d, want %d", code, ExitRefused)
	}

	want := "the specification was refused:\n" +
		"  field \"row.on\": unique is not supported for boolean fields; the domain holds two values\n" +
		"  field \"row.day\": unique needs 4 distinct values but 2026-01-01..2026-01-02 provides 2 days\n"
	if stderr != want {
		t.Errorf("stderr =\n%q\nwant\n%q", stderr, want)
	}
}

func TestAMissingSpecFileIsRefused(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.json")

	code, _, stderr := runCLI(t, Deps{}, "-spec", missing)
	if code != ExitRefused {
		t.Fatalf("exit code = %d, want %d", code, ExitRefused)
	}
	if !strings.HasPrefix(stderr, "the specification was refused: read spec: ") {
		t.Errorf("stderr = %q, want it to name the read failure", stderr)
	}
}

func TestModelFailuresUseTheModelExitCode(t *testing.T) {
	tests := []struct {
		name string
		deps Deps
		want string
	}{
		{
			name: "no compiler is configured",
			deps: Deps{},
			want: "the model could not be used: no compiler is configured",
		},
		{
			name: "the environment is missing the key",
			deps: Deps{NewCompiler: func(context.Context) (agent.Compiler, error) {
				return nil, agent.ErrMissingAPIKey
			}},
			want: "the model could not be used: missing GOOGLE_API_KEY",
		},
		{
			name: "the model itself failed",
			deps: Deps{NewCompiler: func(context.Context) (agent.Compiler, error) {
				return agent.Fake{Err: errors.New("quota exceeded")}, nil
			}},
			want: "the model could not be used: quota exceeded",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := runCLI(t, tt.deps, "-doc", "anything")

			if code != ExitModel {
				t.Fatalf("exit code = %d, want %d", code, ExitModel)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want nothing", stdout)
			}
			if strings.TrimSpace(stderr) != tt.want {
				t.Errorf("stderr = %q, want %q", strings.TrimSpace(stderr), tt.want)
			}
		})
	}
}

func TestACompiledSpecTheContractRefusesIsARefusal(t *testing.T) {
	deps := Deps{NewCompiler: func(context.Context) (agent.Compiler, error) {
		return agent.Fake{Spec: spec.DatasetSpec{
			Name: "compiled",
			Entities: []spec.Entity{{
				Name:   "flag",
				Count:  3,
				Fields: []spec.Field{{Name: "on", Type: spec.TypeBoolean, Unique: true}},
			}},
		}}, nil
	}}

	code, stdout, stderr := runCLI(t, deps, "-doc", "three booleans")

	if code != ExitRefused {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, ExitRefused, stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing", stdout)
	}

	want := "the specification was refused:\n" +
		"  field \"flag.on\": unique is not supported for boolean fields; the domain holds two values\n"
	if stderr != want {
		t.Errorf("stderr =\n%q\nwant\n%q", stderr, want)
	}
}

func TestAViolatedRuleReportsADefectAndPrintsNoRecords(t *testing.T) {
	deps := Deps{Validate: func(spec.DatasetSpec, spec.SyntheticDataset) spec.ValidationReport {
		return spec.ValidationReport{
			Passed: false,
			Total:  2,
			Checked: []spec.Check{
				{Rule: "range", Subject: `field "customer.age"`, Detail: "5/6 values within [18, 90]"},
				{Rule: "count", Subject: `entity "customer"`, Detail: "6 of 6 records", Passed: true},
			},
		}
	}}

	code, stdout, stderr := runCLI(t, deps, "-spec", fixturePath("support-tickets.json"))

	if code != ExitViolation {
		t.Fatalf("exit code = %d, want %d", code, ExitViolation)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing: a batch that broke a rule is not printed", stdout)
	}

	want := "the batch broke 1 of 2 declared rules:\n" +
		"  range field \"customer.age\": 5/6 values within [18, 90]\n" +
		"this is a defect in this repository, not in the specification\n"
	if stderr != want {
		t.Errorf("stderr =\n%q\nwant\n%q", stderr, want)
	}
}

func TestThePreviewCapsWhatItPrintsButTheJSONDoesNot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "many.json")
	const many = `{
  "name": "many",
  "seed": 4,
  "entities": [
    {
      "name": "row",
      "count": 23,
      "fields": [{"name": "id", "type": "string", "unique": true, "pattern": "R-{seq:4}"}]
    }
  ]
}`
	if err := os.WriteFile(path, []byte(many), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	code, stdout, stderr := runCLI(t, Deps{}, "-spec", path)
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, ExitOK, stderr)
	}
	if want := "... 3 more records (use -json for the whole batch)"; !strings.Contains(stdout, want) {
		t.Errorf("stdout does not contain %q\noutput:\n%s", want, stdout)
	}

	_, asJSON, _ := runCLI(t, Deps{}, "-spec", path, "-json")
	dataset := decodeDataset(t, asJSON)
	if got := len(dataset.Entities[0].Records); got != 23 {
		t.Errorf("the JSON output holds %d records, want all 23", got)
	}
}
