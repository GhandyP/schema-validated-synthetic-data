// Package generate turns a validated specification into records.
//
// The generator is deterministic: the same spec and the same seed always produce
// the same records, byte for byte. It is also the only place records come from —
// the model is never asked for a value — so a declared rule cannot be violated by
// a creative answer. What cannot be satisfied is reported as an error instead of
// being approximated.
package generate

import (
	"fmt"
	"hash/fnv"
	"math"
	"math/bits"
	"math/rand/v2"
	"strconv"
	"strings"

	"github.com/GhandyP/schema-validated-synthetic-data/internal/spec"
)

// maxExactCapacity bounds the offset space the generator samples exactly. Beyond
// it the ranges involved are so wide that exact sampling would cost more than the
// batch is worth, and the spec is refused rather than approximated.
const maxExactCapacity = 1 << 62

// nullableRatioDenominator means one record in four is null on a nullable field.
const nullableRatioDenominator = 4

// Records builds the whole batch. Records follow the order the spec declared;
// entities are generated in dependency order, parents before children.
func Records(s spec.DatasetSpec) (spec.SyntheticDataset, error) {
	if err := s.Validate(); err != nil {
		return spec.SyntheticDataset{}, err
	}

	order, err := generationOrder(s)
	if err != nil {
		return spec.SyntheticDataset{}, err
	}

	generated := make(map[string]spec.EntityData, len(order))
	for _, name := range order {
		entity, ok := s.Entity(name)
		if !ok {
			return spec.SyntheticDataset{}, fmt.Errorf("entity %q is missing from the spec", name)
		}

		data, err := generateEntity(s, entity, generated)
		if err != nil {
			return spec.SyntheticDataset{}, err
		}
		generated[name] = data
	}

	dataset := spec.SyntheticDataset{
		Spec:        s,
		Seed:        s.Seed,
		Synthetic:   true,
		Entities:    make([]spec.EntityData, 0, len(s.Entities)),
		Assumptions: assumptions(s),
	}
	for _, entity := range s.Entities {
		dataset.Entities = append(dataset.Entities, generated[entity.Name])
	}
	return dataset, nil
}

// generationOrder returns entity names with every parent before its children, in
// spec order among independent entities so the result is stable.
func generationOrder(s spec.DatasetSpec) ([]string, error) {
	const (
		unvisited = iota
		visiting
		visited
	)

	state := make(map[string]int, len(s.Entities))
	order := make([]string, 0, len(s.Entities))

	var visit func(name string) error
	visit = func(name string) error {
		switch state[name] {
		case visited:
			return nil
		case visiting:
			return fmt.Errorf("entity %q participates in a reference cycle", name)
		}
		state[name] = visiting

		entity, ok := s.Entity(name)
		if !ok {
			return fmt.Errorf("entity %q is referenced but not declared", name)
		}
		for _, field := range entity.Fields {
			if field.Type != spec.TypeReference {
				continue
			}
			target, ok := field.ReferenceEntity()
			if !ok {
				continue
			}
			if err := visit(target); err != nil {
				return err
			}
		}

		state[name] = visited
		order = append(order, name)
		return nil
	}

	for _, entity := range s.Entities {
		if err := visit(entity.Name); err != nil {
			return nil, err
		}
	}
	return order, nil
}

func generateEntity(s spec.DatasetSpec, e spec.Entity, generated map[string]spec.EntityData) (spec.EntityData, error) {
	names := e.FieldNames()
	records := make([]spec.Record, e.Count)
	for i := range records {
		records[i] = spec.NewRecord(names)
	}

	for _, field := range e.Fields {
		stream := newStream(s.Seed, e.Name, field.Name)

		values, err := fieldValues(s, e, field, stream, generated)
		if err != nil {
			return spec.EntityData{}, fmt.Errorf("field %q: %w", e.Name+"."+field.Name, err)
		}
		if len(values) != e.Count {
			return spec.EntityData{}, fmt.Errorf("field %q: generated %d values for %d records", e.Name+"."+field.Name, len(values), e.Count)
		}

		for i, record := range records {
			value := values[i]
			if field.Nullable && stream.IntN(nullableRatioDenominator) == 0 {
				value = nil
			}
			record.Set(field.Name, value)
		}
	}

	return spec.EntityData{Name: e.Name, Records: records}, nil
}

// fieldValues produces every value of one field, in record order.
func fieldValues(s spec.DatasetSpec, e spec.Entity, f spec.Field, stream *rand.Rand, generated map[string]spec.EntityData) ([]any, error) {
	switch f.Type {
	case spec.TypeBoolean:
		values := make([]any, e.Count)
		for i := range values {
			values[i] = stream.IntN(2) == 0
		}
		return values, nil

	case spec.TypeInteger:
		return integerValues(f, e.Count, stream)

	case spec.TypeNumber:
		values := make([]any, e.Count)
		for i := range values {
			values[i] = numberValue(stream, *f.Min, *f.Max)
		}
		return values, nil

	case spec.TypeDate:
		return dateValues(f, e.Count, stream)

	case spec.TypeEnum:
		return choiceValues(f, e.Count, stream)

	case spec.TypeString:
		if len(f.Values) > 0 {
			return choiceValues(f, e.Count, stream)
		}
		values := make([]any, e.Count)
		for i := range values {
			values[i] = renderPattern(f.Pattern, stream, i)
		}
		return values, nil

	case spec.TypeReference:
		return referenceValues(e, f, e.Count, stream, generated)

	default:
		return nil, fmt.Errorf("type %q is not supported", f.Type)
	}
}

func integerValues(f spec.Field, count int, stream *rand.Rand) ([]any, error) {
	minimum, maximum := *f.Min, *f.Max

	values := make([]any, count)
	if !f.Unique {
		for i := range values {
			values[i] = intValue(stream, minimum, maximum)
		}
		return values, nil
	}

	if capacity := maximum - minimum + 1; capacity > maxExactCapacity {
		return nil, fmt.Errorf("the range [%g, %g] is too wide to sample distinct values exactly; narrow min and max", minimum, maximum)
	}
	offsets, err := distinctOffsets(stream, int64(maximum-minimum+1), count)
	if err != nil {
		return nil, err
	}
	for i, offset := range offsets {
		values[i] = int64(minimum) + offset
	}
	return values, nil
}

func dateValues(f spec.Field, count int, stream *rand.Rand) ([]any, error) {
	minimum, err := spec.ParseDate(f.MinDate)
	if err != nil {
		return nil, fmt.Errorf("min_date %q is not a date in YYYY-MM-DD form", f.MinDate)
	}
	maximum, err := spec.ParseDate(f.MaxDate)
	if err != nil {
		return nil, fmt.Errorf("max_date %q is not a date in YYYY-MM-DD form", f.MaxDate)
	}

	days := int64(maximum.Sub(minimum).Hours()/24) + 1
	values := make([]any, count)

	if !f.Unique {
		for i := range values {
			values[i] = minimum.AddDate(0, 0, int(stream.Int64N(days))).Format(spec.DateFormat)
		}
		return values, nil
	}

	offsets, err := distinctOffsets(stream, days, count)
	if err != nil {
		return nil, err
	}
	for i, offset := range offsets {
		values[i] = minimum.AddDate(0, 0, int(offset)).Format(spec.DateFormat)
	}
	return values, nil
}

func choiceValues(f spec.Field, count int, stream *rand.Rand) ([]any, error) {
	values := make([]any, count)
	if !f.Unique {
		for i := range values {
			values[i] = f.Values[stream.IntN(len(f.Values))]
		}
		return values, nil
	}

	offsets, err := distinctOffsets(stream, int64(len(f.Values)), count)
	if err != nil {
		return nil, err
	}
	for i, offset := range offsets {
		values[i] = f.Values[offset]
	}
	return values, nil
}

func referenceValues(e spec.Entity, f spec.Field, count int, stream *rand.Rand, generated map[string]spec.EntityData) ([]any, error) {
	targetName, _ := f.ReferenceEntity()
	targetField, _ := f.ReferenceField()

	target, ok := generated[targetName]
	if !ok {
		return nil, fmt.Errorf("entity %q must be generated before %q", targetName, e.Name)
	}

	keys := make([]any, 0, len(target.Records))
	for _, record := range target.Records {
		if value, ok := record.Value(targetField); ok && value != nil {
			keys = append(keys, value)
		}
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("entity %q produced no value in %q to reference", targetName, targetField)
	}

	values := make([]any, count)
	if !f.Unique {
		for i := range values {
			values[i] = keys[stream.IntN(len(keys))]
		}
		return values, nil
	}

	offsets, err := distinctOffsets(stream, int64(len(keys)), count)
	if err != nil {
		return nil, err
	}
	for i, offset := range offsets {
		values[i] = keys[offset]
	}
	return values, nil
}

// distinctOffsets draws count distinct offsets from [0, capacity).
//
// The identity permutation is materialized only where the shuffle touches it, so
// the cost is proportional to count rather than to capacity: a range of a million
// allowed values costs the same as a range of ten.
func distinctOffsets(stream *rand.Rand, capacity int64, count int) ([]int64, error) {
	if capacity < int64(count) {
		return nil, fmt.Errorf("%d distinct values are needed but the domain provides %d", count, capacity)
	}

	swapped := make(map[int64]int64, count*2)
	at := func(i int64) int64 {
		if value, ok := swapped[i]; ok {
			return value
		}
		return i
	}

	offsets := make([]int64, count)
	for i := int64(0); i < int64(count); i++ {
		j := i + stream.Int64N(capacity-i)
		current, candidate := at(i), at(j)
		swapped[i], swapped[j] = candidate, current
		offsets[i] = candidate
	}
	return offsets, nil
}

func intValue(stream *rand.Rand, minimum, maximum float64) int64 {
	if maximum <= minimum {
		return int64(minimum)
	}
	value := minimum + math.Floor(stream.Float64()*(maximum-minimum+1))
	return int64(math.Min(math.Max(value, minimum), maximum))
}

// numberValue draws a value in [minimum, maximum] with two decimals. Rounding can
// step outside the bounds, so the result is clamped: the declared range is the
// promise, and it survives the rounding.
func numberValue(stream *rand.Rand, minimum, maximum float64) float64 {
	if maximum <= minimum {
		return minimum
	}
	value := math.Round((minimum+stream.Float64()*(maximum-minimum))*100) / 100
	return math.Min(math.Max(value, minimum), maximum)
}

// renderPattern fills the tokens of a string pattern. spec.Validate has already
// refused unknown tokens, so everything left is implemented here.
func renderPattern(pattern string, stream *rand.Rand, index int) string {
	var b strings.Builder
	rest := pattern

	for {
		start := strings.IndexByte(rest, '{')
		if start < 0 {
			b.WriteString(rest)
			break
		}
		b.WriteString(rest[:start])

		end := strings.IndexByte(rest[start:], '}')
		if end < 0 {
			b.WriteString(rest[start:])
			break
		}

		token := rest[start : start+end+1]
		b.WriteString(tokenValue(token, stream, index))
		rest = rest[start+end+1:]
	}
	return b.String()
}

func tokenValue(token string, stream *rand.Rand, index int) string {
	switch {
	case token == "{seq}":
		return strconv.Itoa(index + 1)
	case strings.HasPrefix(token, "{seq:"):
		width, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(token, "{seq:"), "}"))
		if err != nil {
			return token
		}
		return fmt.Sprintf("%0*d", width, index+1)
	case token == "{word}":
		return words[stream.IntN(len(words))]
	default:
		return token
	}
}

// newStream derives an independent stream from the seed and the identity of the
// field being generated, so a value cannot depend on how many other values were
// drawn before it.
func newStream(seed int64, entity, field string) *rand.Rand {
	hash := fnv.New64a()
	fmt.Fprintf(hash, "%d\x1f%s\x1f%s", seed, entity, field)

	sum := hash.Sum64()
	return rand.New(rand.NewPCG(sum, bits.RotateLeft64(sum, 31)^0x9E3779B97F4A7C15))
}

// assumptions explains the parts of the batch a reader cannot see in the values:
// what the compiler could not express, what nullability exempts, and where the
// wording comes from.
func assumptions(s spec.DatasetSpec) []string {
	out := append([]string(nil), s.Assumptions...)

	for _, entity := range s.Entities {
		for _, field := range entity.Fields {
			name := entity.Name + "." + field.Name
			if field.Nullable {
				out = append(out, fmt.Sprintf("field %q is nullable: null values are exempt from its range, uniqueness, and reference rules", name))
			}
			if field.Sensitive {
				out = append(out, fmt.Sprintf("field %q is marked sensitive: its values were generated from this spec and never sampled from a real record", name))
			}
			if field.Type == spec.TypeString && strings.Contains(field.Pattern, "{word}") {
				out = append(out, fmt.Sprintf("field %q uses the built-in synthetic word list, so its wording is templated rather than natural", name))
			}
		}
	}
	return out
}
