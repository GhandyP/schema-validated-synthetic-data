package spec

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// baseSpec is the reference specification: it satisfies every static rule, and
// each test below changes exactly one thing about it. The same shape is used by
// testdata/support-tickets.json, so a rule that changes here changes there.
func baseSpec() DatasetSpec {
	return DatasetSpec{
		Name: "support_tickets",
		Seed: 20260920,
		Entities: []Entity{
			{
				Name:  "customer",
				Count: 3,
				Fields: []Field{
					{Name: "id", Type: TypeString, Unique: true, Pattern: "C-{seq:4}"},
					{Name: "tier", Type: TypeEnum, Values: []string{"free", "pro", "enterprise"}},
					{Name: "age", Type: TypeInteger, Min: number(18), Max: number(90)},
					{Name: "signed_up", Type: TypeDate, MinDate: "2024-01-01", MaxDate: "2026-09-01"},
					{Name: "active", Type: TypeBoolean},
					{Name: "score", Type: TypeNumber, Min: number(0), Max: number(100), Nullable: true},
					{Name: "email", Type: TypeString, Sensitive: true, Pattern: "user{seq:4}@example.test"},
				},
			},
			{
				Name:  "ticket",
				Count: 6,
				Fields: []Field{
					{Name: "id", Type: TypeString, Unique: true, Pattern: "T-{seq:4}"},
					{Name: "customer_id", Type: TypeReference, References: "customer.id"},
					{Name: "priority", Type: TypeEnum, Values: []string{"low", "medium", "high"}},
					{Name: "opened_at", Type: TypeDate, MinDate: "2026-01-01", MaxDate: "2026-09-30"},
				},
			},
		},
	}
}

func number(value float64) *float64 { return &value }

func entityAt(s *DatasetSpec, name string) *Entity {
	for i := range s.Entities {
		if s.Entities[i].Name == name {
			return &s.Entities[i]
		}
	}
	return nil
}

func fieldAt(s *DatasetSpec, entity, field string) *Field {
	e := entityAt(s, entity)
	if e == nil {
		return nil
	}
	for i := range e.Fields {
		if e.Fields[i].Name == field {
			return &e.Fields[i]
		}
	}
	return nil
}

func TestBaseSpecSatisfiesEveryStaticRule(t *testing.T) {
	if err := baseSpec().Validate(); err != nil {
		t.Fatalf("baseSpec().Validate() error = %v, want nil", err)
	}
}

func TestValidateRefusesImpossibleSpecsAndNamesEveryProblem(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*DatasetSpec)
		want   []string
	}{
		{
			name:   "name is required",
			mutate: func(s *DatasetSpec) { s.Name = "  " },
			want:   []string{"name is required"},
		},
		{
			name:   "at least one entity",
			mutate: func(s *DatasetSpec) { s.Entities = nil },
			want:   []string{"at least one entity is required"},
		},
		{
			name:   "entity name is required",
			mutate: func(s *DatasetSpec) { entityAt(s, "ticket").Name = "" },
			want:   []string{"entity name is required"},
		},
		{
			name:   "duplicate entity",
			mutate: func(s *DatasetSpec) { entityAt(s, "ticket").Name = "customer" },
			want:   []string{`duplicate entity "customer"`},
		},
		{
			name:   "count must be positive",
			mutate: func(s *DatasetSpec) { entityAt(s, "ticket").Count = 0 },
			want:   []string{`entity "ticket": count must be greater than zero`},
		},
		{
			name:   "entity needs a field",
			mutate: func(s *DatasetSpec) { entityAt(s, "ticket").Fields = nil },
			want:   []string{`entity "ticket": at least one field is required`},
		},
		{
			name:   "field name is required",
			mutate: func(s *DatasetSpec) { fieldAt(s, "customer", "id").Name = "" },
			want:   []string{`entity "customer": field name is required`},
		},
		{
			name: "duplicate field",
			mutate: func(s *DatasetSpec) {
				e := entityAt(s, "ticket")
				e.Fields = append(e.Fields, Field{Name: "id", Type: TypeString, Unique: true, Pattern: "X-{seq}"})
			},
			want: []string{`entity "ticket": duplicate field "id"`},
		},
		{
			name:   "unknown type",
			mutate: func(s *DatasetSpec) { *fieldAt(s, "customer", "tier") = Field{Name: "tier", Type: "uuid"} },
			want: []string{
				`field "customer.tier": type "uuid" is not one of string, integer, number, boolean, date, enum, reference`,
			},
		},
		{
			name:   "string needs a declared domain",
			mutate: func(s *DatasetSpec) { fieldAt(s, "customer", "email").Pattern = "" },
			want: []string{
				`field "customer.email": values or pattern is required for a string field, for example "USR-{seq:4}"`,
			},
		},
		{
			name:   "pattern refuses an unsupported token",
			mutate: func(s *DatasetSpec) { fieldAt(s, "customer", "id").Pattern = "C-{guid}" },
			want: []string{
				`field "customer.id": pattern contains the unsupported token {guid}; supported tokens are {seq}, {seq:W} and {word}`,
			},
		},
		{
			name:   "pattern refuses an unbalanced token",
			mutate: func(s *DatasetSpec) { fieldAt(s, "customer", "id").Pattern = "C-{seq" },
			want:   []string{`field "customer.id": pattern "C-{seq" has an unbalanced token`},
		},
		{
			name:   "pattern width must be numeric",
			mutate: func(s *DatasetSpec) { fieldAt(s, "customer", "id").Pattern = "C-{seq:ab}" },
			want:   []string{`field "customer.id": pattern token {seq:ab} needs a numeric width of up to 18 digits`},
		},
		{
			name:   "unique is impossible for boolean",
			mutate: func(s *DatasetSpec) { fieldAt(s, "customer", "active").Unique = true },
			want:   []string{`field "customer.active": unique is not supported for boolean fields; the domain holds two values`},
		},
		{
			name:   "unique is impossible for number",
			mutate: func(s *DatasetSpec) { fieldAt(s, "customer", "score").Unique = true },
			want: []string{
				`field "customer.score": unique is not supported for number fields; a continuous range cannot prove distinct values, so use an integer field`,
			},
		},
		{
			name: "unique integer needs range capacity",
			mutate: func(s *DatasetSpec) {
				field := fieldAt(s, "customer", "age")
				field.Min, field.Max, field.Unique = number(1), number(2), true
			},
			want: []string{`field "customer.age": unique needs 3 distinct values but the range [1, 2] provides 2`},
		},
		{
			name:   "integer min must be whole",
			mutate: func(s *DatasetSpec) { fieldAt(s, "customer", "age").Min = number(18.5) },
			want:   []string{`field "customer.age": min must be a whole number for an integer field`},
		},
		{
			name: "integer min must not exceed max",
			mutate: func(s *DatasetSpec) {
				field := fieldAt(s, "customer", "age")
				field.Min, field.Max = number(90), number(18)
			},
			want: []string{`field "customer.age": min must not be greater than max`},
		},
		{
			name:   "integer min is required",
			mutate: func(s *DatasetSpec) { fieldAt(s, "customer", "age").Min = nil },
			want:   []string{`field "customer.age": min is required for an integer field`},
		},
		{
			name:   "number max is required",
			mutate: func(s *DatasetSpec) { fieldAt(s, "customer", "score").Max = nil },
			want:   []string{`field "customer.score": max is required for a number field`},
		},
		{
			name:   "date must be well formed",
			mutate: func(s *DatasetSpec) { fieldAt(s, "customer", "signed_up").MinDate = "2026-13-01" },
			want:   []string{`field "customer.signed_up": min_date "2026-13-01" is not a date in YYYY-MM-DD form`},
		},
		{
			name:   "date min must not be after max",
			mutate: func(s *DatasetSpec) { fieldAt(s, "customer", "signed_up").MinDate = "2026-09-02" },
			want:   []string{`field "customer.signed_up": min_date must not be after max_date`},
		},
		{
			name: "unique date needs day capacity",
			mutate: func(s *DatasetSpec) {
				field := fieldAt(s, "customer", "signed_up")
				field.Unique, field.MinDate, field.MaxDate = true, "2026-01-01", "2026-01-02"
			},
			want: []string{`field "customer.signed_up": unique needs 3 distinct values but 2026-01-01..2026-01-02 provides 2 days`},
		},
		{
			name:   "enum needs values",
			mutate: func(s *DatasetSpec) { *fieldAt(s, "ticket", "priority") = Field{Name: "priority", Type: TypeEnum} },
			want:   []string{`field "ticket.priority": values is required for an enum field`},
		},
		{
			name:   "unique enum needs enough values",
			mutate: func(s *DatasetSpec) { fieldAt(s, "ticket", "priority").Unique = true },
			want:   []string{`field "ticket.priority": unique needs 6 distinct values but values provides 3`},
		},
		{
			name:   "enum values must be distinct",
			mutate: func(s *DatasetSpec) { fieldAt(s, "ticket", "priority").Values = []string{"low", "low", "high"} },
			want:   []string{`field "ticket.priority": values contains duplicate "low"`},
		},
		{
			name:   "numeric bounds on a date are refused",
			mutate: func(s *DatasetSpec) { fieldAt(s, "customer", "signed_up").Min = number(1) },
			want:   []string{`field "customer.signed_up": min and max are only for integer and number fields`},
		},
		{
			name:   "date bounds on an integer are refused",
			mutate: func(s *DatasetSpec) { fieldAt(s, "customer", "age").MinDate = "2026-01-01" },
			want:   []string{`field "customer.age": min_date and max_date are only for date fields`},
		},
		{
			name:   "values on an integer are refused",
			mutate: func(s *DatasetSpec) { fieldAt(s, "customer", "age").Values = []string{"1", "2"} },
			want:   []string{`field "customer.age": values is only for enum and string fields`},
		},
		{
			name:   "pattern on an integer is refused",
			mutate: func(s *DatasetSpec) { fieldAt(s, "customer", "age").Pattern = "A-{seq}" },
			want:   []string{`field "customer.age": pattern is only for string fields`},
		},
		{
			name:   "references on a string are refused",
			mutate: func(s *DatasetSpec) { fieldAt(s, "customer", "id").References = "customer.id" },
			want:   []string{`field "customer.id": references is only for reference fields`},
		},
		{
			name:   "reference target needs two parts",
			mutate: func(s *DatasetSpec) { fieldAt(s, "ticket", "customer_id").References = "customer" },
			want:   []string{`field "ticket.customer_id": references "customer" must have the form "entity.field"`},
		},
		{
			name:   "reference to an unknown entity",
			mutate: func(s *DatasetSpec) { fieldAt(s, "ticket", "customer_id").References = "buyer.id" },
			want:   []string{`field "ticket.customer_id": references unknown entity "buyer"`},
		},
		{
			name:   "reference to an unknown field",
			mutate: func(s *DatasetSpec) { fieldAt(s, "ticket", "customer_id").References = "customer.missing" },
			want:   []string{`field "ticket.customer_id": references unknown field "customer.missing"`},
		},
		{
			name:   "reference to a non-unique field",
			mutate: func(s *DatasetSpec) { fieldAt(s, "ticket", "customer_id").References = "customer.tier" },
			want: []string{
				`field "ticket.customer_id": references "customer.tier", which is not unique; a foreign key must point at distinct values`,
			},
		},
		{
			name: "reference cycle",
			mutate: func(s *DatasetSpec) {
				e := entityAt(s, "customer")
				e.Fields = append(e.Fields, Field{Name: "ticket_id", Type: TypeReference, References: "ticket.id"})
			},
			want: []string{`entity "ticket": references create a cycle: customer -> ticket -> customer`},
		},
		{
			name: "self reference",
			mutate: func(s *DatasetSpec) {
				e := entityAt(s, "customer")
				e.Fields = append(e.Fields, Field{Name: "parent_id", Type: TypeReference, References: "customer.id"})
			},
			want: []string{`entity "customer": references create a cycle: customer -> customer`},
		},
		{
			name:   "unique reference needs parent capacity",
			mutate: func(s *DatasetSpec) { fieldAt(s, "ticket", "customer_id").Unique = true },
			want:   []string{`field "ticket.customer_id": unique needs 6 distinct values but entity "customer" has 3 records`},
		},
		{
			name: "every problem is reported in spec order",
			mutate: func(s *DatasetSpec) {
				age := fieldAt(s, "customer", "age")
				age.Min, age.Max, age.Unique = number(1), number(2), true
				fieldAt(s, "ticket", "priority").Unique = true
			},
			want: []string{
				`field "customer.age": unique needs 3 distinct values but the range [1, 2] provides 2`,
				`field "ticket.priority": unique needs 6 distinct values but values provides 3`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := baseSpec()
			tt.mutate(&s)

			err := s.Validate()
			if err == nil {
				t.Fatalf("Validate() error = nil, want %d problem(s)", len(tt.want))
			}

			var problems Problems
			if !errors.As(err, &problems) {
				t.Fatalf("Validate() error = %v, want a spec.Problems value", err)
			}
			if !reflect.DeepEqual([]string(problems), tt.want) {
				t.Errorf("problems =\n  %q\nwant\n  %q", problems, tt.want)
			}
			if got, want := problems.Error(), strings.Join(tt.want, "; "); got != want {
				t.Errorf("problems.Error() = %q, want %q", got, want)
			}
		})
	}
}

func TestValidateReportsCapacityBoundariesAsValid(t *testing.T) {
	// The capacity rule is a boundary: exactly enough distinct values is legal,
	// one fewer is not. Both sides are checked so the rule cannot drift.
	tests := []struct {
		name    string
		count   int
		minimum float64
		maximum float64
		want    string
	}{
		{name: "exact capacity is accepted", count: 3, minimum: 1, maximum: 3},
		{
			name:    "one short is refused",
			count:   4,
			minimum: 1,
			maximum: 3,
			want:    `field "customer.age": unique needs 4 distinct values but the range [1, 3] provides 3`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := baseSpec()
			entityAt(&s, "customer").Count = tt.count
			field := fieldAt(&s, "customer", "age")
			field.Min, field.Max, field.Unique = number(tt.minimum), number(tt.maximum), true

			err := s.Validate()
			if tt.want == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v, want nil", err)
				}
				return
			}
			if err == nil || err.Error() != tt.want {
				t.Fatalf("Validate() error = %v, want %q", err, tt.want)
			}
		})
	}
}
