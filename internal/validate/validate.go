// Package validate re-checks a generated batch against the specification it came
// from.
//
// This package deliberately shares no code with the generator. It reads the spec
// again and asks independently whether each declared rule holds, so a bug in the
// generator shows up as a failed check instead of being reproduced by the thing
// meant to catch it. It never trusts a value's history: only the value, the spec,
// and the other generated values decide the verdict.
package validate

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/GhandyP/schema-validated-synthetic-data/internal/spec"
)

// Dataset re-checks every rule the specification declares and returns the report.
//
// A violated rule is a finding, not an error: the caller decides what a failed
// report means, and the report itself stays complete.
func Dataset(s spec.DatasetSpec, dataset spec.SyntheticDataset) spec.ValidationReport {
	v := &validator{
		spec:    s,
		dataset: dataset,
		byName:  make(map[string]spec.EntityData, len(dataset.Entities)),
	}
	for _, data := range dataset.Entities {
		v.byName[data.Name] = data
	}

	v.checkLabel()
	v.checkSeed()
	v.checkEntities()

	return v.report()
}

type validator struct {
	spec    spec.DatasetSpec
	dataset spec.SyntheticDataset
	byName  map[string]spec.EntityData
	checks  []spec.Check
}

// add records one rule and its verdict. The word order of the detail is the same
// for a pass and for a failure, so a reader can compare them at a glance.
func (v *validator) add(rule, subject, detail string, passed bool) {
	v.checks = append(v.checks, spec.Check{Rule: rule, Subject: subject, Detail: detail, Passed: passed})
}

func (v *validator) report() spec.ValidationReport {
	report := spec.ValidationReport{Passed: true, Total: len(v.checks), Checked: v.checks}
	for _, check := range v.checks {
		if !check.Passed {
			report.Passed = false
			break
		}
	}
	return report
}

func (v *validator) checkLabel() {
	detail := "the payload declares itself synthetic"
	if !v.dataset.Synthetic {
		detail = "the payload does not declare itself synthetic"
	}
	v.add("label", "dataset", detail, v.dataset.Synthetic)
}

func (v *validator) checkSeed() {
	detail := fmt.Sprintf("the recorded seed %d is the spec seed", v.dataset.Seed)
	if v.dataset.Seed != v.spec.Seed {
		detail = fmt.Sprintf("the recorded seed %d is not the spec seed %d", v.dataset.Seed, v.spec.Seed)
	}
	v.add("seed", "dataset", detail, v.dataset.Seed == v.spec.Seed)
}

// checkEntities verifies the dataset holds exactly the declared entities, in the
// declared order. A missing or invented entity would make every per-entity check
// below meaningless, so it is reported first.
func (v *validator) checkEntities() {
	got := make([]string, 0, len(v.dataset.Entities))
	for _, data := range v.dataset.Entities {
		got = append(got, data.Name)
	}

	want := make([]string, 0, len(v.spec.Entities))
	for _, entity := range v.spec.Entities {
		want = append(want, entity.Name)
	}

	v.add("entities", "dataset",
		fmt.Sprintf("the dataset holds %d of the %d declared entities", len(got), len(want)),
		equalStrings(got, want))

	for _, entity := range v.spec.Entities {
		data, ok := v.byName[entity.Name]
		if !ok {
			v.add("count", entitySubject(entity), fmt.Sprintf("0 of %d records", entity.Count), false)
			continue
		}
		v.checkEntity(entity, data)
	}
}

func (v *validator) checkEntity(entity spec.Entity, data spec.EntityData) {
	v.checkCount(entity, data)
	v.checkShape(entity, data)

	for _, field := range entity.Fields {
		v.checkField(entity, field, data.Records)
	}
}

// entitySubject names an entity exactly as the spec package names it, so a
// refusal and a failed check read alike.
func entitySubject(entity spec.Entity) string {
	return fmt.Sprintf("entity %q", entity.Name)
}

func (v *validator) checkCount(entity spec.Entity, data spec.EntityData) {
	v.add("count", entitySubject(entity),
		fmt.Sprintf("%d of %d records", len(data.Records), entity.Count),
		len(data.Records) == entity.Count)
}

func (v *validator) checkShape(entity spec.Entity, data spec.EntityData) {
	want := entity.FieldNames()

	offending := -1
	for i, record := range data.Records {
		if !equalStrings(record.Fields, want) {
			offending = i
			break
		}
	}

	detail := fmt.Sprintf("every record lists the %d declared fields in order", len(want))
	if offending >= 0 {
		detail = fmt.Sprintf("record %d does not list the %d declared fields in order", offending, len(want))
	}
	v.add("shape", entitySubject(entity), detail, offending < 0)
}

func (v *validator) checkField(entity spec.Entity, field spec.Field, records []spec.Record) {
	subject := fmt.Sprintf("field %q", entity.Name+"."+field.Name)

	v.checkNullability(subject, field, records)

	if field.Type == spec.TypeReference {
		v.checkReference(subject, entity, field, records)
		return
	}

	v.checkType(subject, field, records)
	v.checkBounds(subject, field, records)
	v.checkDomain(subject, field, records)

	if field.Unique {
		v.checkUnique(subject, records, field.Name)
	}
}

func (v *validator) checkNullability(subject string, field spec.Field, records []spec.Record) {
	if field.Nullable {
		return
	}

	nulls := 0
	for _, record := range records {
		if isNull(record, field.Name) {
			nulls++
		}
	}

	detail := "no null values in a non-nullable field"
	switch {
	case nulls == 1:
		detail = "1 null value in a non-nullable field"
	case nulls > 1:
		detail = fmt.Sprintf("%d null values in a non-nullable field", nulls)
	}
	v.add("nullability", subject, detail, nulls == 0)
}

func (v *validator) checkType(subject string, field spec.Field, records []spec.Record) {
	typeName, ok := v.concreteType(field)
	if !ok {
		v.add("type", subject, "the reference chain does not end in a value type", false)
		return
	}

	matching := 0
	for _, record := range records {
		if isNull(record, field.Name) {
			continue
		}
		if value, _ := record.Value(field.Name); valueMatchesType(typeName, value) {
			matching++
		}
	}

	v.add("type", subject,
		fmt.Sprintf("%d/%d values are %s", matching, countNonNull(records, field.Name), typeName),
		matching == countNonNull(records, field.Name))
}

func (v *validator) checkBounds(subject string, field spec.Field, records []spec.Record) {
	switch field.Type {
	case spec.TypeInteger:
		outside := 0
		for _, record := range records {
			value, ok := record.Value(field.Name)
			if !ok || value == nil {
				continue
			}
			number, ok := value.(int64)
			if !ok || float64(number) < *field.Min || float64(number) > *field.Max {
				outside++
			}
		}
		v.add("range", subject,
			fmt.Sprintf("%d/%d values within [%g, %g]", countNonNull(records, field.Name)-outside, countNonNull(records, field.Name), *field.Min, *field.Max),
			outside == 0)

	case spec.TypeNumber:
		outside := 0
		for _, record := range records {
			value, ok := record.Value(field.Name)
			if !ok || value == nil {
				continue
			}
			number, ok := value.(float64)
			if !ok || number < *field.Min || number > *field.Max {
				outside++
			}
		}
		v.add("range", subject,
			fmt.Sprintf("%d/%d values within [%g, %g]", countNonNull(records, field.Name)-outside, countNonNull(records, field.Name), *field.Min, *field.Max),
			outside == 0)

	case spec.TypeDate:
		outside := 0
		for _, record := range records {
			value, ok := record.Value(field.Name)
			if !ok || value == nil {
				continue
			}
			date, ok := value.(string)
			if !ok || date < field.MinDate || date > field.MaxDate {
				outside++
			}
		}
		v.add("range", subject,
			fmt.Sprintf("%d/%d values within [%s, %s]", countNonNull(records, field.Name)-outside, countNonNull(records, field.Name), field.MinDate, field.MaxDate),
			outside == 0)
	}
}

func (v *validator) checkDomain(subject string, field spec.Field, records []spec.Record) {
	switch {
	case field.Type == spec.TypeEnum || (field.Type == spec.TypeString && len(field.Values) > 0):
		allowed := make(map[string]bool, len(field.Values))
		for _, value := range field.Values {
			allowed[value] = true
		}

		outside := 0
		for _, record := range records {
			value, ok := record.Value(field.Name)
			if !ok || value == nil {
				continue
			}
			text, ok := value.(string)
			if !ok || !allowed[text] {
				outside++
			}
		}
		count := countNonNull(records, field.Name)
		v.add("values", subject,
			fmt.Sprintf("%d/%d values in the declared set of %d", count-outside, count, len(field.Values)),
			outside == 0)

	case field.Type == spec.TypeString && field.Pattern != "":
		expression := patternRegexp(field.Pattern)

		outside := 0
		for _, record := range records {
			value, ok := record.Value(field.Name)
			if !ok || value == nil {
				continue
			}
			text, ok := value.(string)
			if !ok || !expression.MatchString(text) {
				outside++
			}
		}
		count := countNonNull(records, field.Name)
		v.add("pattern", subject,
			fmt.Sprintf("%d/%d values match %q", count-outside, count, field.Pattern),
			outside == 0)
	}
}

// checkUnique counts distinct non-null values. Nulls are exempt by declaration:
// the nullable flag is what allows them, and the generator documents that
// exemption in the assumptions.
func (v *validator) checkUnique(subject string, records []spec.Record, field string) {
	seen := make(map[any]bool, len(records))
	count := 0

	for _, record := range records {
		if isNull(record, field) {
			continue
		}
		value, _ := record.Value(field)
		count++
		seen[value] = true
	}

	v.add("unique", subject,
		fmt.Sprintf("%d/%d values are distinct", len(seen), count),
		len(seen) == count)
}

func (v *validator) checkReference(subject string, entity spec.Entity, field spec.Field, records []spec.Record) {
	targetName, _ := field.ReferenceEntity()
	targetField, _ := field.ReferenceField()

	target, ok := v.byName[targetName]
	if !ok {
		v.add("reference", subject,
			fmt.Sprintf("entity %q is missing from the dataset", targetName), false)
		return
	}

	keys := make(map[any]bool, len(target.Records))
	for _, record := range target.Records {
		if isNull(record, targetField) {
			continue
		}
		value, _ := record.Value(targetField)
		keys[value] = true
	}

	resolved := 0
	count := 0
	for _, record := range records {
		if isNull(record, field.Name) {
			continue
		}
		count++
		value, _ := record.Value(field.Name)
		if keys[value] {
			resolved++
		}
	}

	v.add("reference", subject,
		fmt.Sprintf("%d/%d values resolve to a key of %s", resolved, count, field.References),
		resolved == count)
}

// concreteType resolves a reference chain to the value type it ultimately copies.
func (v *validator) concreteType(field spec.Field) (string, bool) {
	for depth := 0; field.Type == spec.TypeReference; depth++ {
		if depth > len(v.spec.Entities) {
			return "", false
		}

		entityName, ok := field.ReferenceEntity()
		if !ok {
			return "", false
		}
		fieldName, ok := field.ReferenceField()
		if !ok {
			return "", false
		}
		entity, ok := v.spec.Entity(entityName)
		if !ok {
			return "", false
		}
		next, ok := entity.Field(fieldName)
		if !ok {
			return "", false
		}
		field = next
	}

	switch field.Type {
	case spec.TypeInteger:
		return "int64", true
	case spec.TypeNumber:
		return "float64", true
	case spec.TypeBoolean:
		return "bool", true
	case spec.TypeString, spec.TypeEnum, spec.TypeDate:
		return "string", true
	default:
		return "", false
	}
}

// patternRegexp turns a declared pattern into the shape a generated value must
// have. It is written here rather than shared with the generator on purpose: the
// check has to be able to disagree with the thing it checks.
func patternRegexp(pattern string) *regexp.Regexp {
	var b strings.Builder
	b.WriteString("^")

	rest := pattern
	for {
		start := strings.IndexByte(rest, '{')
		if start < 0 {
			b.WriteString(regexp.QuoteMeta(rest))
			break
		}
		b.WriteString(regexp.QuoteMeta(rest[:start]))

		end := strings.IndexByte(rest[start:], '}')
		if end < 0 {
			b.WriteString(regexp.QuoteMeta(rest[start:]))
			break
		}

		token := rest[start : start+end+1]
		switch {
		case token == "{seq}":
			b.WriteString(`[1-9][0-9]*`)
		case strings.HasPrefix(token, "{seq:"):
			width, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(token, "{seq:"), "}"))
			if err != nil || width < 1 {
				b.WriteString(`[0-9]+`)
				break
			}
			fmt.Fprintf(&b, `[0-9]{%d,}`, width)
		case token == "{word}":
			b.WriteString(`[a-z]+`)
		default:
			b.WriteString(regexp.QuoteMeta(token))
		}
		rest = rest[start+end+1:]
	}

	b.WriteString("$")
	return regexp.MustCompile(b.String())
}

func isNull(record spec.Record, field string) bool {
	value, ok := record.Value(field)
	return !ok || value == nil
}

func countNonNull(records []spec.Record, field string) int {
	count := 0
	for _, record := range records {
		if !isNull(record, field) {
			count++
		}
	}
	return count
}

func valueMatchesType(typeName string, value any) bool {
	switch typeName {
	case "int64":
		_, ok := value.(int64)
		return ok
	case "float64":
		_, ok := value.(float64)
		return ok
	case "bool":
		_, ok := value.(bool)
		return ok
	case "string":
		_, ok := value.(string)
		return ok
	default:
		return false
	}
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
