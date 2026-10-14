package spec

import (
	"fmt"
	"math"
	"regexp"
	"strings"
)

// Problems is the complete list of static rules a specification violates.
//
// It is returned whole, not one problem at a time, so a caller fixing a spec
// sees every mistake in a single run.
type Problems []string

// Error renders the problems on one line.
func (p Problems) Error() string {
	return strings.Join(p, "; ")
}

// OrNil returns nil when nothing was recorded, so a caller can return an empty
// Problems value as a nil error.
func (p Problems) OrNil() error {
	if len(p) == 0 {
		return nil
	}
	return p
}

// Validate applies every static rule of this package and returns all violations.
//
// The rules exist to make one promise possible: a spec that Validate accepts can
// be generated so that every declared rule provably holds. A request that cannot
// be satisfied is refused here, before any record exists.
func (s DatasetSpec) Validate() error {
	problems := Problems{}

	if strings.TrimSpace(s.Name) == "" {
		problems = append(problems, "name is required")
	}
	if len(s.Entities) == 0 {
		problems = append(problems, "at least one entity is required")
	}

	seen := make(map[string]bool, len(s.Entities))
	for _, entity := range s.Entities {
		switch {
		case strings.TrimSpace(entity.Name) == "":
			problems = append(problems, "entity name is required")
		case seen[entity.Name]:
			problems = append(problems, fmt.Sprintf("duplicate entity %q", entity.Name))
		}
		seen[entity.Name] = true
	}

	for _, entity := range s.Entities {
		problems = append(problems, entity.validate()...)
	}

	// Reference resolution needs entity and field names that already resolve, so
	// the shape of the spec is settled first.
	if len(problems) > 0 {
		return problems
	}
	return append(problems, s.validateReferences()...).OrNil()
}

func (e Entity) validate() Problems {
	if strings.TrimSpace(e.Name) == "" {
		return nil
	}

	problems := Problems{}
	subject := fmt.Sprintf("entity %q", e.Name)

	if e.Count <= 0 {
		problems = append(problems, subject+": count must be greater than zero")
	}
	if len(e.Fields) == 0 {
		problems = append(problems, subject+": at least one field is required")
	}

	seen := make(map[string]bool, len(e.Fields))
	for _, field := range e.Fields {
		switch {
		case strings.TrimSpace(field.Name) == "":
			problems = append(problems, subject+": field name is required")
			continue
		case seen[field.Name]:
			problems = append(problems, fmt.Sprintf("%s: duplicate field %q", subject, field.Name))
		}
		seen[field.Name] = true

		problems = append(problems, field.validate(e)...)
	}
	return problems
}

// validate applies the rules of one field. Every branch below is one way a
// specification can be impossible, and the message names the field so a refusal
// is actionable without reading this file.
func (f Field) validate(e Entity) Problems {
	problems := Problems{}
	subject := fmt.Sprintf("field %q", e.Name+"."+f.Name)
	add := func(format string, args ...any) {
		problems = append(problems, subject+": "+fmt.Sprintf(format, args...))
	}

	// Bounds and closed domains belong to specific types. A key that does not
	// apply is an error rather than a silent no-op, so nobody believes a bound
	// was honoured when the generator ignored it.
	if !f.IsNumeric() && (f.Min != nil || f.Max != nil) {
		add("min and max are only for integer and number fields")
	}
	if f.Type != TypeDate && (f.MinDate != "" || f.MaxDate != "") {
		add("min_date and max_date are only for date fields")
	}
	if f.Type != TypeEnum && f.Type != TypeString && len(f.Values) > 0 {
		add("values is only for enum and string fields")
	}
	if f.Type != TypeString && f.Pattern != "" {
		add("pattern is only for string fields")
	}
	if f.Type != TypeReference && f.References != "" {
		add("references is only for reference fields")
	}
	if duplicate, ok := duplicateValue(f.Values); ok {
		add("values contains duplicate %q", duplicate)
	}

	switch f.Type {
	case TypeString:
		if len(f.Values) == 0 && f.Pattern == "" {
			add(`values or pattern is required for a string field, for example "USR-{seq:4}"`)
		}
		patternErr := checkPattern(f.Pattern)
		if patternErr != nil {
			add("%s", patternErr)
		}

		// Uniqueness is only judged against a domain that can actually produce
		// values: a pattern already refused above is reported once, not twice.
		if f.Unique {
			switch {
			case len(f.Values) > 0:
				if distinct := len(distinctValues(f.Values)); distinct < e.Count {
					add("unique needs %d distinct values but values provides %d", e.Count, distinct)
				}
			case patternErr != nil:
			case hasSequenceToken(f.Pattern):
			default:
				add("unique requires values or a pattern containing {seq}")
			}
		}

	case TypeInteger:
		if f.Min == nil {
			add("min is required for an integer field")
		}
		if f.Max == nil {
			add("max is required for an integer field")
		}
		if f.Min != nil && !isWhole(*f.Min) {
			add("min must be a whole number for an integer field")
		}
		if f.Max != nil && !isWhole(*f.Max) {
			add("max must be a whole number for an integer field")
		}
		if f.Min != nil && f.Max != nil {
			if *f.Max < *f.Min {
				add("min must not be greater than max")
			} else if f.Unique {
				if capacity := *f.Max - *f.Min + 1; capacity < float64(e.Count) {
					add("unique needs %d distinct values but the range [%g, %g] provides %d", e.Count, *f.Min, *f.Max, int64(capacity))
				}
			}
		}

	case TypeNumber:
		if f.Min == nil {
			add("min is required for a number field")
		}
		if f.Max == nil {
			add("max is required for a number field")
		}
		if f.Min != nil && f.Max != nil && *f.Max < *f.Min {
			add("min must not be greater than max")
		}
		if f.Unique {
			add("unique is not supported for number fields; a continuous range cannot prove distinct values, so use an integer field")
		}

	case TypeBoolean:
		if f.Unique {
			add("unique is not supported for boolean fields; the domain holds two values")
		}

	case TypeDate:
		if f.MinDate == "" {
			add("min_date is required for a date field")
		}
		if f.MaxDate == "" {
			add("max_date is required for a date field")
		}
		minTime, minErr := ParseDate(f.MinDate)
		if f.MinDate != "" && minErr != nil {
			add("min_date %q is not a date in YYYY-MM-DD form", f.MinDate)
		}
		maxTime, maxErr := ParseDate(f.MaxDate)
		if f.MaxDate != "" && maxErr != nil {
			add("max_date %q is not a date in YYYY-MM-DD form", f.MaxDate)
		}
		if minErr == nil && maxErr == nil {
			if maxTime.Before(minTime) {
				add("min_date must not be after max_date")
			} else if f.Unique {
				days := int64(maxTime.Sub(minTime).Hours()/24) + 1
				if days < int64(e.Count) {
					add("unique needs %d distinct values but %s..%s provides %d days", e.Count, f.MinDate, f.MaxDate, days)
				}
			}
		}

	case TypeEnum:
		if len(f.Values) == 0 {
			add("values is required for an enum field")
		} else if f.Unique {
			if distinct := len(distinctValues(f.Values)); distinct < e.Count {
				add("unique needs %d distinct values but values provides %d", e.Count, distinct)
			}
		}

	case TypeReference:
		if f.References == "" {
			add("references is required for a reference field")
		} else if _, _, ok := splitReference(f.References); !ok {
			add("references %q must have the form \"entity.field\"", f.References)
		}

	default:
		add("type %q is not one of %s", f.Type, strings.Join(FieldTypes(), ", "))
	}

	return problems
}

// validateReferences resolves every reference against the entities that exist and
// refuses cycles, which a generator that must know a parent before its children
// cannot honour.
func (s DatasetSpec) validateReferences() Problems {
	problems := Problems{}

	for _, entity := range s.Entities {
		for _, field := range entity.Fields {
			if field.Type != TypeReference {
				continue
			}

			subject := fmt.Sprintf("field %q", entity.Name+"."+field.Name)
			add := func(format string, args ...any) {
				problems = append(problems, subject+": "+fmt.Sprintf(format, args...))
			}

			targetName, ok := field.ReferenceEntity()
			if !ok {
				continue // already reported as a malformed target
			}
			targetFieldName, _ := field.ReferenceField()

			target, ok := s.Entity(targetName)
			if !ok {
				add("references unknown entity %q", targetName)
				continue
			}
			targetField, ok := target.Field(targetFieldName)
			if !ok {
				add("references unknown field %q", field.References)
				continue
			}
			if !targetField.Unique {
				add("references %q, which is not unique; a foreign key must point at distinct values", field.References)
			}
			if field.Unique && target.Count < entity.Count {
				add("unique needs %d distinct values but entity %q has %d records", entity.Count, target.Name, target.Count)
			}
		}
	}

	return append(problems, s.validateCycles()...)
}

// validateCycles walks the reference graph and reports the first cycle found for
// each entity that closes one. Iteration follows spec order, so the report is
// stable for a given spec.
func (s DatasetSpec) validateCycles() Problems {
	const (
		unvisited = iota
		visiting
		visited
	)

	state := make(map[string]int, len(s.Entities))
	reported := make(map[string]bool)
	path := make([]string, 0, len(s.Entities))
	problems := Problems{}

	var walk func(name string)
	walk = func(name string) {
		state[name] = visiting
		path = append(path, name)

		entity, ok := s.Entity(name)
		if !ok {
			path = path[:len(path)-1]
			state[name] = visited
			return
		}

		for _, field := range entity.Fields {
			if field.Type != TypeReference {
				continue
			}
			target, ok := field.ReferenceEntity()
			if !ok {
				continue
			}

			switch state[target] {
			case visiting:
				cycle := append(append([]string(nil), path[indexOf(path, target):]...), target)
				key := strings.Join(cycle, " -> ")
				if !reported[key] {
					reported[key] = true
					problems = append(problems, fmt.Sprintf("entity %q: references create a cycle: %s", name, key))
				}
			case unvisited:
				if _, ok := s.Entity(target); ok {
					walk(target)
				}
			}
		}

		path = path[:len(path)-1]
		state[name] = visited
	}

	for _, entity := range s.Entities {
		if entity.Name == "" || state[entity.Name] != unvisited {
			continue
		}
		walk(entity.Name)
	}

	return problems
}

// patternToken matches the closed tokens a string pattern may contain.
var patternToken = regexp.MustCompile(`\{[^{}]*\}`)

// checkPattern refuses any token the generator does not implement, so a pattern
// that intends a value it will never receive fails here instead of at run time.
func checkPattern(pattern string) error {
	if pattern == "" {
		return nil
	}

	tokens := patternToken.FindAllString(pattern, -1)
	if strings.Count(pattern, "{") != len(tokens) || strings.Count(pattern, "}") != len(tokens) {
		return fmt.Errorf("pattern %q has an unbalanced token", pattern)
	}

	for _, token := range tokens {
		switch {
		case token == "{seq}" || token == "{word}":
			continue
		case strings.HasPrefix(token, "{seq:") && strings.HasSuffix(token, "}"):
			if width := strings.TrimSuffix(strings.TrimPrefix(token, "{seq:"), "}"); !isDigits(width) {
				return fmt.Errorf("pattern token %s needs a numeric width of up to 18 digits", token)
			}
		default:
			return fmt.Errorf("pattern contains the unsupported token %s; supported tokens are {seq}, {seq:W} and {word}", token)
		}
	}
	return nil
}

// hasSequenceToken reports whether a pattern embeds the record sequence, which is
// what makes uniqueness provable by construction rather than by retrying. Both
// {seq} and the padded {seq:W} form qualify.
func hasSequenceToken(pattern string) bool {
	for _, token := range patternToken.FindAllString(pattern, -1) {
		if token == "{seq}" || strings.HasPrefix(token, "{seq:") {
			return true
		}
	}
	return false
}

func isDigits(value string) bool {
	if len(value) == 0 || len(value) > 18 {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func isWhole(value float64) bool {
	return math.Trunc(value) == value
}

func distinctValues(values []string) []string {
	seen := make(map[string]bool, len(values))
	distinct := make([]string, 0, len(values))
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		distinct = append(distinct, value)
	}
	return distinct
}

func duplicateValue(values []string) (string, bool) {
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if seen[value] {
			return value, true
		}
		seen[value] = true
	}
	return "", false
}

func indexOf(values []string, want string) int {
	for i, value := range values {
		if value == want {
			return i
		}
	}
	return 0
}

// FieldTypes lists the field types this package accepts, in the order they are
// reported. The ADK output schema is built from this list, so the model is
// offered exactly the types the validator accepts.
func FieldTypes() []string {
	return []string{
		string(TypeString),
		string(TypeInteger),
		string(TypeNumber),
		string(TypeBoolean),
		string(TypeDate),
		string(TypeEnum),
		string(TypeReference),
	}
}
