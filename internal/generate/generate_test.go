package generate

import (
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/GhandyP/schema-validated-synthetic-data/internal/spec"
)

func number(value float64) *float64 { return &value }

// supportSpec is the reference batch used by most tests here: two entities, a
// foreign key, a closed enum, a numeric range, a date range, a boolean, a nullable
// column, a sensitive column, and uniqueness in three different domains.
func supportSpec() spec.DatasetSpec {
	return spec.DatasetSpec{
		Name:        "support_tickets",
		Seed:        20260920,
		Assumptions: []string{"the request did not state how many customers exist"},
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

func mustRecords(t *testing.T, s spec.DatasetSpec) spec.SyntheticDataset {
	t.Helper()

	dataset, err := Records(s)
	if err != nil {
		t.Fatalf("Records() error = %v, want nil", err)
	}
	return dataset
}

func valueOf(t *testing.T, dataset spec.SyntheticDataset, entity string, index int, field string) any {
	t.Helper()

	for _, data := range dataset.Entities {
		if data.Name != entity {
			continue
		}
		if index >= len(data.Records) {
			t.Fatalf("entity %q has %d records, want index %d", entity, len(data.Records), index)
		}
		value, ok := data.Records[index].Value(field)
		if !ok {
			t.Fatalf("entity %q record %d has no field %q", entity, index, field)
		}
		return value
	}
	t.Fatalf("dataset has no entity %q", entity)
	return nil
}

func recordsOf(t *testing.T, dataset spec.SyntheticDataset, entity string) []spec.Record {
	t.Helper()

	for _, data := range dataset.Entities {
		if data.Name == entity {
			return data.Records
		}
	}
	t.Fatalf("dataset has no entity %q", entity)
	return nil
}

func TestTheSameSpecAndSeedProduceTheSameBytes(t *testing.T) {
	first, err := json.Marshal(mustRecords(t, supportSpec()))
	if err != nil {
		t.Fatalf("marshal first batch: %v", err)
	}
	second, err := json.Marshal(mustRecords(t, supportSpec()))
	if err != nil {
		t.Fatalf("marshal second batch: %v", err)
	}

	if string(first) != string(second) {
		t.Fatalf("two runs of the same spec and seed differ:\nfirst  %.400s\nsecond %.400s", first, second)
	}
}

func TestADifferentSeedProducesADifferentBatch(t *testing.T) {
	first := mustRecords(t, supportSpec())

	other := supportSpec()
	other.Seed++

	second := mustRecords(t, other)

	if reflect.DeepEqual(first.Entities, second.Entities) {
		t.Fatal("changing the seed did not change any record")
	}
}

func TestEveryValueRespectsItsDeclaredTypeAndRange(t *testing.T) {
	dataset := mustRecords(t, supportSpec())

	for i := range 4 {
		if id, ok := valueOf(t, dataset, "customer", i, "id").(string); !ok || !strings.HasPrefix(id, "C-") {
			t.Errorf("customer %d id = %v, want a string with the C- prefix", i, id)
		}
		if tier, ok := valueOf(t, dataset, "customer", i, "tier").(string); !ok || !slices.Contains([]string{"free", "pro", "enterprise"}, tier) {
			t.Errorf("customer %d tier = %v, want one of the declared values", i, tier)
		}
		if age, ok := valueOf(t, dataset, "customer", i, "age").(int64); !ok || age < 18 || age > 90 {
			t.Errorf("customer %d age = %v, want an int64 within [18, 90]", i, age)
		}
		if signed, ok := valueOf(t, dataset, "customer", i, "signed_up").(string); !ok || signed < "2024-01-01" || signed > "2026-09-01" {
			t.Errorf("customer %d signed_up = %v, want a date within the declared range", i, signed)
		}
		if _, ok := valueOf(t, dataset, "customer", i, "active").(bool); !ok {
			t.Errorf("customer %d active = %T, want bool", i, valueOf(t, dataset, "customer", i, "active"))
		}
		if score := valueOf(t, dataset, "customer", i, "score"); score != nil {
			if value, ok := score.(float64); !ok || value < 0 || value > 100 {
				t.Errorf("customer %d score = %v, want nil or a float64 within [0, 100]", i, score)
			}
		}
	}

	for i := range 8 {
		if opened, ok := valueOf(t, dataset, "ticket", i, "opened_at").(string); !ok || opened < "2026-01-01" || opened > "2026-09-30" {
			t.Errorf("ticket %d opened_at = %v, want a date within the declared range", i, opened)
		}
		if priority, ok := valueOf(t, dataset, "ticket", i, "priority").(string); !ok || !slices.Contains([]string{"low", "medium", "high"}, priority) {
			t.Errorf("ticket %d priority = %v, want one of the declared values", i, priority)
		}
	}
}

func TestUniqueFieldsAreDistinct(t *testing.T) {
	dataset := mustRecords(t, supportSpec())

	for _, data := range dataset.Entities {
		seen := make(map[string]bool, len(data.Records))
		for i, record := range data.Records {
			value, ok := record.Value("id")
			if !ok {
				t.Fatalf("entity %q record %d has no id", data.Name, i)
			}
			id, ok := value.(string)
			if !ok {
				t.Fatalf("entity %q record %d id = %T, want string", data.Name, i, value)
			}
			if seen[id] {
				t.Errorf("entity %q repeated the unique value %q", data.Name, id)
			}
			seen[id] = true
		}
	}
}

func TestUniqueEnumUsesEveryDeclaredValueWhenCapacityIsExact(t *testing.T) {
	s := spec.DatasetSpec{
		Name: "seats",
		Seed: 7,
		Entities: []spec.Entity{{
			Name:  "seat",
			Count: 3,
			Fields: []spec.Field{
				{Name: "row", Type: spec.TypeEnum, Unique: true, Values: []string{"A", "B", "C"}},
			},
		}},
	}

	dataset := mustRecords(t, s)

	got := make([]string, 0, 3)
	for i := range 3 {
		value, ok := valueOf(t, dataset, "seat", i, "row").(string)
		if !ok {
			t.Fatalf("seat %d row is not a string", i)
		}
		got = append(got, value)
	}

	slices.Sort(got)
	if want := []string{"A", "B", "C"}; !slices.Equal(got, want) {
		t.Fatalf("row values = %v, want every declared value exactly once %v", got, want)
	}
}

func TestUniqueIntegerSamplesExactOffsetsAcrossAWideRange(t *testing.T) {
	s := spec.DatasetSpec{
		Name: "wide",
		Seed: 11,
		Entities: []spec.Entity{{
			Name:  "row",
			Count: 5,
			Fields: []spec.Field{
				{Name: "id", Type: spec.TypeInteger, Unique: true, Min: number(0), Max: number(1_000_000_000)},
			},
		}},
	}

	dataset := mustRecords(t, s)

	seen := make(map[int64]bool, 5)
	for i := range 5 {
		value, ok := valueOf(t, dataset, "row", i, "id").(int64)
		if !ok || value < 0 || value > 1_000_000_000 {
			t.Fatalf("row %d id = %v, want an int64 within the declared range", i, value)
		}
		if seen[value] {
			t.Fatalf("row %d repeated the unique value %d", i, value)
		}
		seen[value] = true
	}
}

func TestReferencesResolveToGeneratedParentKeys(t *testing.T) {
	dataset := mustRecords(t, supportSpec())

	parentIDs := make(map[string]bool)
	for i := range 4 {
		id, ok := valueOf(t, dataset, "customer", i, "id").(string)
		if !ok {
			t.Fatalf("customer %d id is not a string", i)
		}
		parentIDs[id] = true
	}

	for i := range 8 {
		key, ok := valueOf(t, dataset, "ticket", i, "customer_id").(string)
		if !ok || !parentIDs[key] {
			t.Errorf("ticket %d customer_id = %v, want one of the generated customer ids", i, key)
		}
	}
}

func TestNullableFieldsProduceNullsAndExemptThemFromTheOtherRules(t *testing.T) {
	s := supportSpec()
	for i := range s.Entities {
		if s.Entities[i].Name == "customer" {
			s.Entities[i].Count = 64
			s.Entities[i].Fields = append(s.Entities[i].Fields, spec.Field{
				Name: "seat", Type: spec.TypeInteger, Unique: true, Nullable: true, Min: number(1), Max: number(200),
			})
		}
	}

	dataset := mustRecords(t, s)

	nulls, seats := 0, make(map[int64]bool)
	for i := range 64 {
		if valueOf(t, dataset, "customer", i, "score") == nil {
			nulls++
		}
		seat := valueOf(t, dataset, "customer", i, "seat")
		if seat == nil {
			continue
		}
		value, ok := seat.(int64)
		if !ok || value < 1 || value > 200 {
			t.Fatalf("customer %d seat = %v, want nil or an int64 within [1, 200]", i, seat)
		}
		if seats[value] {
			t.Fatalf("customer %d repeated the unique seat %d", i, value)
		}
		seats[value] = true
	}

	if nulls == 0 {
		t.Fatal("a nullable field produced no null across 64 records")
	}
}

func TestAssumptionsExplainWhatTheValuesCannotShow(t *testing.T) {
	dataset := mustRecords(t, supportSpec())

	want := []string{
		"the request did not state how many customers exist",
		`field "customer.score" is nullable: null values are exempt from its range, uniqueness, and reference rules`,
		`field "customer.email" is marked sensitive: its values were generated from this spec and never sampled from a real record`,
	}
	if !reflect.DeepEqual(dataset.Assumptions, want) {
		t.Errorf("assumptions =\n%q\nwant\n%q", dataset.Assumptions, want)
	}
	if !dataset.Synthetic {
		t.Error("dataset.Synthetic = false, want true: the output must label itself synthetic")
	}
}

func TestTemplatedWordingIsDeclaredAsAnAssumption(t *testing.T) {
	s := spec.DatasetSpec{
		Name: "words",
		Seed: 3,
		Entities: []spec.Entity{{
			Name:   "row",
			Count:  2,
			Fields: []spec.Field{{Name: "label", Type: spec.TypeString, Pattern: "team-{word}"}},
		}},
	}

	dataset := mustRecords(t, s)

	want := `field "row.label" uses the built-in synthetic word list, so its wording is templated rather than natural`
	if len(dataset.Assumptions) != 1 || dataset.Assumptions[0] != want {
		t.Errorf("assumptions = %q, want [%q]", dataset.Assumptions, want)
	}
}

func TestRecordsEmitFieldsInDeclaredOrder(t *testing.T) {
	dataset := mustRecords(t, supportSpec())

	encoded, err := json.Marshal(recordsOf(t, dataset, "customer")[0])
	if err != nil {
		t.Fatalf("marshal record: %v", err)
	}

	position := -1
	for _, field := range []string{"id", "tier", "age", "signed_up", "active", "score", "email"} {
		at := strings.Index(string(encoded), `"`+field+`":`)
		if at <= position {
			t.Fatalf("field %q is not emitted after the previous one: %s", field, encoded)
		}
		position = at
	}
	if !strings.HasPrefix(string(encoded), `{"id":`) {
		t.Errorf("record = %s, want it to start with the first declared field", encoded)
	}
}

func TestPatternRendersLiteralsAndTokens(t *testing.T) {
	s := spec.DatasetSpec{
		Name: "patterns",
		Seed: 5,
		Entities: []spec.Entity{{
			Name:  "row",
			Count: 3,
			Fields: []spec.Field{
				{Name: "padded", Type: spec.TypeString, Unique: true, Pattern: "C-{seq:4}"},
				{Name: "plain", Type: spec.TypeString, Pattern: "N{seq}"},
				{Name: "templated", Type: spec.TypeString, Pattern: "team-{word}-{seq}"},
			},
		}},
	}

	dataset := mustRecords(t, s)
	words := Words()

	for i, want := range []string{"C-0001", "C-0002", "C-0003"} {
		if got, _ := valueOf(t, dataset, "row", i, "padded").(string); got != want {
			t.Errorf("row %d padded = %q, want %q", i, got, want)
		}
	}
	for i, want := range []string{"N1", "N2", "N3"} {
		if got, _ := valueOf(t, dataset, "row", i, "plain").(string); got != want {
			t.Errorf("row %d plain = %q, want %q", i, got, want)
		}
	}

	got, _ := valueOf(t, dataset, "row", 0, "templated").(string)
	if !strings.HasPrefix(got, "team-") || !strings.HasSuffix(got, "-1") {
		t.Fatalf("templated = %q, want it to keep the literals around the tokens", got)
	}
	if word := strings.TrimSuffix(strings.TrimPrefix(got, "team-"), "-1"); !slices.Contains(words, word) {
		t.Fatalf("templated = %q, want its word to come from the built-in list", got)
	}
}

func TestParentsAreGeneratedBeforeTheirChildrenWhateverTheDeclarationOrder(t *testing.T) {
	s := spec.DatasetSpec{
		Name: "chain",
		Seed: 13,
		Entities: []spec.Entity{
			{
				Name:  "ticket",
				Count: 2,
				Fields: []spec.Field{
					{Name: "account_id", Type: spec.TypeReference, References: "account.id"},
				},
			},
			{
				Name:  "account",
				Count: 2,
				Fields: []spec.Field{
					{Name: "id", Type: spec.TypeString, Unique: true, Pattern: "A-{seq}"},
					{Name: "customer_id", Type: spec.TypeReference, References: "customer.id"},
				},
			},
			{
				Name:  "customer",
				Count: 2,
				Fields: []spec.Field{
					{Name: "id", Type: spec.TypeString, Unique: true, Pattern: "K-{seq}"},
				},
			},
		},
	}

	dataset := mustRecords(t, s)

	names := make([]string, 0, 3)
	for _, data := range dataset.Entities {
		names = append(names, data.Name)
	}
	if want := []string{"ticket", "account", "customer"}; !slices.Equal(names, want) {
		t.Errorf("output order = %v, want the declared order %v", names, want)
	}

	accounts := make(map[string]bool)
	for i := range 2 {
		id, _ := valueOf(t, dataset, "customer", i, "id").(string)
		accounts[id] = true
	}
	for i := range 2 {
		key, _ := valueOf(t, dataset, "account", i, "customer_id").(string)
		if !accounts[key] {
			t.Errorf("account %d customer_id = %q, want a generated customer id", i, key)
		}
	}
}

func TestRecordsRefusesASpecItCannotSatisfy(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*spec.DatasetSpec)
		want   string
	}{
		{
			name: "unique enum with too few values",
			mutate: func(s *spec.DatasetSpec) {
				s.Entities[0].Count = 5
				s.Entities[0].Fields[1].Unique = true
			},
			want: `field "customer.tier": unique needs 5 distinct values but values provides 3`,
		},
		{
			name:   "unique reference without parent capacity",
			mutate: func(s *spec.DatasetSpec) { s.Entities[1].Fields[1].Unique = true },
			want:   `field "ticket.customer_id": unique needs 8 distinct values but entity "customer" has 4 records`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := supportSpec()
			tt.mutate(&s)

			_, err := Records(s)
			if err == nil {
				t.Fatalf("Records() error = nil, want %q", tt.want)
			}
			if got := err.Error(); got != tt.want {
				t.Fatalf("Records() error = %q, want %q", got, tt.want)
			}

			// A refusal must stay inspectable problem by problem: the CLI prints one
			// line per problem, and that only works while the value is a spec.Problems.
			var problems spec.Problems
			if !errors.As(err, &problems) || len(problems) == 0 {
				t.Fatalf("Records() error = %v, want a non-empty spec.Problems value", err)
			}
		})
	}
}
