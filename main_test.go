package main

import (
	"errors"
	"testing"

	"github.com/GhandyP/schema-validated-synthetic-data/internal/spec"
)

// TestTheRunnableFixtureMatchesTheContract pins the example the README tells a
// reader to run. If the contract moves and the fixture is not moved with it, the
// first command in the README stops working, so this test fails instead.
func TestTheRunnableFixtureMatchesTheContract(t *testing.T) {
	declared, err := spec.Load("testdata/support-tickets.json")
	if err != nil {
		t.Fatalf("the README's first command no longer works: %v", err)
	}

	if declared.Name != "support_tickets" {
		t.Errorf("name = %q, want support_tickets", declared.Name)
	}
	if declared.Seed != 20260920 {
		t.Errorf("seed = %d, want the seed the README documents", declared.Seed)
	}
	if len(declared.Entities) != 2 {
		t.Fatalf("the fixture declares %d entities, want a parent and a child", len(declared.Entities))
	}
	if len(declared.Assumptions) == 0 {
		t.Error("the fixture declares no assumptions, want the example to show what a batch does not claim")
	}

	ticket, ok := declared.Entity("ticket")
	if !ok {
		t.Fatal("the fixture has no ticket entity")
	}
	customerID, ok := ticket.Field("customer_id")
	if !ok || customerID.References != "customer.id" {
		t.Fatalf("ticket.customer_id = %+v, want a reference to customer.id", customerID)
	}

	// The example has to exercise the paths a reader is being taught: uniqueness
	// with a pattern, a closed domain, a bounded range, a bounded date, a nullable
	// column, and a column marked as sensitive.
	for _, want := range []struct {
		entity, field string
		check         func(spec.Field) bool
		description   string
	}{
		{"customer", "id", func(f spec.Field) bool { return f.Unique && f.Pattern != "" }, "a unique patterned key"},
		{"customer", "tier", func(f spec.Field) bool { return len(f.Values) > 0 }, "a closed domain"},
		{"customer", "age", func(f spec.Field) bool { return f.Min != nil && f.Max != nil }, "a bounded range"},
		{"customer", "signed_up", func(f spec.Field) bool { return f.MinDate != "" }, "a bounded date"},
		{"customer", "satisfaction", func(f spec.Field) bool { return f.Nullable }, "a nullable column"},
		{"customer", "email", func(f spec.Field) bool { return f.Sensitive }, "a column resembling personal data"},
	} {
		entity, ok := declared.Entity(want.entity)
		if !ok {
			t.Fatalf("the fixture has no %q entity", want.entity)
		}
		field, ok := entity.Field(want.field)
		if !ok {
			t.Fatalf("the fixture has no %q field", want.entity+"."+want.field)
		}
		if !want.check(field) {
			t.Errorf("%s.%s does not demonstrate %s", want.entity, want.field, want.description)
		}
	}
}

// TestTheRefusedFixturesAreRefusedForTheReasonTheyName keeps the three refusal
// examples honest: each one has to fail for the single rule it is meant to teach,
// with the message the README reports.
func TestTheRefusedFixturesAreRefusedForTheReasonTheyName(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "a unique boolean",
			path: "testdata/impossible/unique-boolean.json",
			want: `field "flag.on": unique is not supported for boolean fields; the domain holds two values`,
		},
		{
			name: "a date range too narrow for the batch",
			path: "testdata/impossible/unique-date-range.json",
			want: `field "visit.day": unique needs 5 distinct values but 2026-01-01..2026-01-03 provides 3 days`,
		},
		{
			name: "a reference cycle",
			path: "testdata/impossible/reference-cycle.json",
			want: `entity "node": references create a cycle: node -> node`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := spec.Load(tt.path)
			if err == nil {
				t.Fatalf("Load(%s) error = nil, want a refusal", tt.path)
			}

			// The fixture is refused for one reason only, and the path is named, so
			// the CLI output a reader sees matches the README.
			want := tt.path + ": " + tt.want
			if err.Error() != want {
				t.Fatalf("Load(%s) error = %q, want %q", tt.path, err, want)
			}

			var problems spec.Problems
			if !errors.As(err, &problems) || len(problems) != 1 {
				t.Fatalf("Load(%s) reported %d problems, want exactly 1", tt.path, len(problems))
			}
		})
	}
}
