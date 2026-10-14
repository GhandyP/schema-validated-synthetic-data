package validate

import (
	"testing"

	"github.com/GhandyP/schema-validated-synthetic-data/internal/generate"
	"github.com/GhandyP/schema-validated-synthetic-data/internal/spec"
)

func number(value float64) *float64 { return &value }

func supportSpec() spec.DatasetSpec {
	return spec.DatasetSpec{
		Name: "support_tickets",
		Seed: 20260920,
		Entities: []spec.Entity{
			{
				Name:  "customer",
				Count: 4,
				Fields: []spec.Field{
					{Name: "id", Type: spec.TypeString, Unique: true, Pattern: "C-{seq:4}"},
					{Name: "tier", Type: spec.TypeEnum, Values: []string{"free", "pro", "enterprise"}},
					{Name: "age", Type: spec.TypeInteger, Min: number(18), Max: number(90)},
					{Name: "signed_up", Type: spec.TypeDate, MinDate: "2024-01-01", MaxDate: "2026-09-01"},
					{Name: "active", Type: spec.TypeBoolean},
					{Name: "score", Type: spec.TypeNumber, Min: number(0), Max: number(100), Nullable: true},
					{Name: "email", Type: spec.TypeString, Sensitive: true, Pattern: "user{seq:4}@example.test"},
				},
			},
			{
				Name:  "ticket",
				Count: 8,
				Fields: []spec.Field{
					{Name: "id", Type: spec.TypeString, Unique: true, Pattern: "T-{seq}"},
					{Name: "customer_id", Type: spec.TypeReference, References: "customer.id"},
					{Name: "priority", Type: spec.TypeEnum, Values: []string{"low", "medium", "high"}},
					{Name: "opened_at", Type: spec.TypeDate, MinDate: "2026-01-01", MaxDate: "2026-09-30"},
				},
			},
		},
	}
}

func cleanDataset(t *testing.T) spec.SyntheticDataset {
	t.Helper()

	dataset, err := generate.Records(supportSpec())
	if err != nil {
		t.Fatalf("generate.Records() error = %v, want nil", err)
	}
	return dataset
}

type failure struct {
	rule    string
	subject string
	detail  string
}

func failuresOf(report spec.ValidationReport) []failure {
	failures := make([]failure, 0, len(report.Checked))
	for _, check := range report.Checked {
		if !check.Passed {
			failures = append(failures, failure{rule: check.Rule, subject: check.Subject, detail: check.Detail})
		}
	}
	return failures
}

func assertFailures(t *testing.T, report spec.ValidationReport, want []failure) {
	t.Helper()

	got := failuresOf(report)
	if len(got) != len(want) {
		t.Fatalf("failed checks = %+v, want exactly %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("failed check %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestGeneratedBatchPassesEveryDeclaredRule is the headline guarantee: what the
// generator produced is re-derived from the spec by different code and holds.
func TestGeneratedBatchPassesEveryDeclaredRule(t *testing.T) {
	report := Dataset(supportSpec(), cleanDataset(t))

	if !report.Passed {
		t.Fatalf("report.Passed = false, failed checks = %+v", failuresOf(report))
	}
	if report.Total != len(report.Checked) {
		t.Errorf("report.Total = %d, want %d (the number of checks it lists)", report.Total, len(report.Checked))
	}

	// The count is asserted exactly: the report claims a specific audit surface,
	// and a rule that silently stops being checked has to fail this test.
	if want := 39; report.Total != want {
		t.Errorf("report.Total = %d, want %d declared rules", report.Total, want)
	}
}

func TestAViewerOfTheReportSeesWhyItFailed(t *testing.T) {
	tests := []struct {
		name   string
		damage func(*spec.SyntheticDataset)
		want   []failure
	}{
		{
			name:   "a value outside its declared range",
			damage: func(d *spec.SyntheticDataset) { d.Entities[0].Records[0].Set("age", int64(200)) },
			want: []failure{{
				rule: "range", subject: `field "customer.age"`, detail: "3/4 values within [18, 90]",
			}},
		},
		{
			name:   "a value of the wrong type",
			damage: func(d *spec.SyntheticDataset) { d.Entities[0].Records[0].Set("age", "ninety") },
			want: []failure{
				{rule: "type", subject: `field "customer.age"`, detail: "3/4 values are int64"},
				{rule: "range", subject: `field "customer.age"`, detail: "3/4 values within [18, 90]"},
			},
		},
		{
			name:   "a value outside the declared set",
			damage: func(d *spec.SyntheticDataset) { d.Entities[0].Records[0].Set("tier", "platinum") },
			want: []failure{{
				rule: "values", subject: `field "customer.tier"`, detail: "3/4 values in the declared set of 3",
			}},
		},
		{
			name: "a value that does not match its pattern",
			// email is not referenced by anything, so the corruption stays local: a
			// failure that cascades could hide the rule this case is about.
			damage: func(d *spec.SyntheticDataset) { d.Entities[0].Records[0].Set("email", "not-an-address") },
			want: []failure{{
				rule: "pattern", subject: `field "customer.email"`, detail: `3/4 values match "user{seq:4}@example.test"`,
			}},
		},
		{
			name:   "a repeated value in a unique field",
			damage: func(d *spec.SyntheticDataset) { d.Entities[0].Records[1].Set("id", mustValue(d, 0, 0, "id")) },
			want: []failure{{
				rule: "unique", subject: `field "customer.id"`, detail: "3/4 values are distinct",
			}},
		},
		{
			name:   "a foreign key that points nowhere",
			damage: func(d *spec.SyntheticDataset) { d.Entities[1].Records[0].Set("customer_id", "C-9999") },
			want: []failure{{
				rule: "reference", subject: `field "ticket.customer_id"`, detail: "7/8 values resolve to a key of customer.id",
			}},
		},
		{
			name:   "a null in a field that does not allow it",
			damage: func(d *spec.SyntheticDataset) { d.Entities[0].Records[0].Set("active", nil) },
			want: []failure{{
				rule: "nullability", subject: `field "customer.active"`, detail: "1 null value in a non-nullable field",
			}},
		},
		{
			name: "a record that does not list the declared fields",
			damage: func(d *spec.SyntheticDataset) {
				fields := d.Entities[0].Records[0].Fields
				fields[0], fields[1] = fields[1], fields[0]
			},
			want: []failure{{
				rule: "shape", subject: `entity "customer"`, detail: "record 0 does not list the 7 declared fields in order",
			}},
		},
		{
			name: "a missing record",
			// The last ticket is dropped instead of a customer: nothing references a
			// ticket, so a dropped customer would break foreign keys as well.
			damage: func(d *spec.SyntheticDataset) { d.Entities[1].Records = d.Entities[1].Records[:7] },
			want: []failure{{
				rule: "count", subject: `entity "ticket"`, detail: "7 of 8 records",
			}},
		},
		{
			name:   "a missing entity",
			damage: func(d *spec.SyntheticDataset) { d.Entities = d.Entities[:1] },
			want: []failure{
				{rule: "entities", subject: "dataset", detail: "the dataset holds 1 of the 2 declared entities"},
				{rule: "count", subject: `entity "ticket"`, detail: "0 of 8 records"},
			},
		},
		{
			name:   "a payload that does not label itself synthetic",
			damage: func(d *spec.SyntheticDataset) { d.Synthetic = false },
			want: []failure{{
				rule: "label", subject: "dataset", detail: "the payload does not declare itself synthetic",
			}},
		},
		{
			name:   "a seed that is not the spec seed",
			damage: func(d *spec.SyntheticDataset) { d.Seed = 7 },
			want: []failure{{
				rule: "seed", subject: "dataset", detail: "the recorded seed 7 is not the spec seed 20260920",
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dataset := cleanDataset(t)
			tt.damage(&dataset)

			report := Dataset(supportSpec(), dataset)
			if report.Passed {
				t.Fatal("report.Passed = true, want false")
			}
			assertFailures(t, report, tt.want)
		})
	}
}

func TestNullsAreExemptFromUniquenessAndRanges(t *testing.T) {
	// A nullable unique field is the case where the exemption matters: the
	// distinct count is compared against the non-null values, not the records.
	s := supportSpec()
	s.Entities[0].Count = 64
	s.Entities[0].Fields = append(s.Entities[0].Fields, spec.Field{
		Name: "seat", Type: spec.TypeInteger, Unique: true, Nullable: true, Min: number(1), Max: number(100),
	})

	dataset, err := generate.Records(s)
	if err != nil {
		t.Fatalf("generate.Records() error = %v, want nil", err)
	}

	report := Dataset(s, dataset)
	if !report.Passed {
		t.Fatalf("report.Passed = false, failed checks = %+v", failuresOf(report))
	}
}

func mustValue(dataset *spec.SyntheticDataset, entity, index int, field string) any {
	value, _ := dataset.Entities[entity].Records[index].Value(field)
	return value
}
