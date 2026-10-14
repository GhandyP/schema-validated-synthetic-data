// Package spec defines the dataset contract of this repository and the static
// rules a specification must satisfy before anything is generated.
//
// Nothing in this package performs I/O, calls a model, or writes a record: it is
// the pure part of the pipeline, and it is where an impossible request is refused.
package spec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"
)

// FieldType enumerates the value domains the generator can produce and the
// validator can re-check.
type FieldType string

// The supported field types.
const (
	TypeString    FieldType = "string"
	TypeInteger   FieldType = "integer"
	TypeNumber    FieldType = "number"
	TypeBoolean   FieldType = "boolean"
	TypeDate      FieldType = "date"
	TypeEnum      FieldType = "enum"
	TypeReference FieldType = "reference"
)

// DateFormat is the only accepted date representation, in the spec and in the
// generated records. It sorts lexicographically, so a range check is a string
// comparison after parsing.
const DateFormat = "2006-01-02"

// DatasetSpec is the typed request the whole pipeline is built on: what the
// caller asked for, compiled into something a program can prove things about.
type DatasetSpec struct {
	// Name identifies the batch in the output and in the preview.
	Name string `json:"name"`
	// Seed starts the deterministic generator. The same spec and the same seed
	// produce the same bytes.
	Seed int64 `json:"seed"`
	// Entities are the tables to generate. A single-entity spec is a flat batch;
	// several entities with references model a small relational fixture.
	Entities []Entity `json:"entities"`
	// Assumptions records what the compiler could not express exactly, so a
	// reader learns what the batch is not claiming.
	Assumptions []string `json:"assumptions,omitempty"`
}

// Entity is one table of the requested batch.
type Entity struct {
	Name   string  `json:"name"`
	Count  int     `json:"count"`
	Fields []Field `json:"fields"`
}

// Field describes one column: its domain, its bounds, and the rules the
// generated values must satisfy.
type Field struct {
	Name string    `json:"name"`
	Type FieldType `json:"type"`

	// Unique demands distinct values within the entity. It is refused whenever
	// the declared domain cannot provably hold Count distinct values.
	Unique bool `json:"unique,omitempty"`
	// Nullable allows null in place of a value. A null skips the range and
	// uniqueness rules that apply to real values.
	Nullable bool `json:"nullable,omitempty"`
	// Sensitive marks a field that resembles personal data. Its values are always
	// generated from this spec and never sampled from a real record.
	Sensitive bool `json:"sensitive,omitempty"`

	// Min and Max bound integer and number fields. Both are required for those
	// types.
	Min *float64 `json:"min,omitempty"`
	Max *float64 `json:"max,omitempty"`

	// MinDate and MaxDate bound date fields with YYYY-MM-DD values.
	MinDate string `json:"min_date,omitempty"`
	MaxDate string `json:"max_date,omitempty"`

	// Values is the closed domain of an enum field, and an optional closed domain
	// for a string field.
	Values []string `json:"values,omitempty"`

	// Pattern builds string values from a template. Supported tokens are {seq},
	// {seq:W} for a zero-padded sequence of width W, and {word} for a value taken
	// from the built-in synthetic word list.
	Pattern string `json:"pattern,omitempty"`

	// References points a reference field at "entity.field" in the same spec.
	References string `json:"references,omitempty"`
}

// ReferenceEntity returns the entity part of a reference target.
func (f Field) ReferenceEntity() (string, bool) {
	entity, _, ok := splitReference(f.References)
	if !ok {
		return "", false
	}
	return entity, true
}

// ReferenceField returns the field part of a reference target.
func (f Field) ReferenceField() (string, bool) {
	_, field, ok := splitReference(f.References)
	if !ok {
		return "", false
	}
	return field, true
}

func splitReference(target string) (string, string, bool) {
	for i := 0; i < len(target); i++ {
		if target[i] != '.' {
			continue
		}
		entity, field := target[:i], target[i+1:]
		if entity == "" || field == "" {
			return "", "", false
		}
		return entity, field, true
	}
	return "", "", false
}

// IsNumeric reports whether the field is bounded by Min and Max.
func (f Field) IsNumeric() bool {
	return f.Type == TypeInteger || f.Type == TypeNumber
}

// EntityData holds the records generated for one entity, in generation order.
type EntityData struct {
	Name    string   `json:"name"`
	Records []Record `json:"records"`
}

// SyntheticDataset is the result of one run: the records, the seed that produced
// them, the assumptions behind them, and the validator's verdict.
type SyntheticDataset struct {
	Spec      DatasetSpec  `json:"spec"`
	Seed      int64        `json:"seed"`
	Synthetic bool         `json:"synthetic"`
	Entities  []EntityData `json:"entities"`

	Assumptions []string          `json:"assumptions,omitempty"`
	Validation  *ValidationReport `json:"validation,omitempty"`
}

// Check is one rule the validator re-derived from the spec, with the verdict it
// reached. Passing checks are kept as well: a report that claims twelve rules were
// checked should be able to list them.
type Check struct {
	Rule    string `json:"rule"`
	Subject string `json:"subject"`
	Detail  string `json:"detail"`
	Passed  bool   `json:"passed"`
}

// ValidationReport is the validator's verdict over one dataset. Total is the
// number of rules checked, so a passing report says how much was proven rather
// than only that nothing was found.
type ValidationReport struct {
	Passed  bool    `json:"passed"`
	Total   int     `json:"total"`
	Checked []Check `json:"checked"`
}

// Record holds one generated row in field order, so the JSON output is stable
// and matches the order the spec declared. A map would marshal in sorted key
// order and lose that.
type Record struct {
	Fields []string
	Values map[string]any
}

// NewRecord returns an empty record that will emit its fields in the given order.
func NewRecord(fields []string) Record {
	return Record{Fields: append([]string(nil), fields...), Values: make(map[string]any, len(fields))}
}

// Set stores a value for a field declared in the record.
func (r Record) Set(name string, value any) {
	r.Values[name] = value
}

// Value returns the stored value for a field and whether it was present.
func (r Record) Value(name string) (any, bool) {
	value, ok := r.Values[name]
	return value, ok
}

// MarshalJSON writes the record's fields in declared order.
func (r Record) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')

	for i, name := range r.Fields {
		if i > 0 {
			b.WriteByte(',')
		}

		key, err := json.Marshal(name)
		if err != nil {
			return nil, fmt.Errorf("marshal field name %q: %w", name, err)
		}
		b.Write(key)
		b.WriteByte(':')

		value, err := json.Marshal(r.Values[name])
		if err != nil {
			return nil, fmt.Errorf("marshal value of field %q: %w", name, err)
		}
		b.Write(value)
	}

	b.WriteByte('}')
	return b.Bytes(), nil
}

// Entity returns the named entity definition.
func (s DatasetSpec) Entity(name string) (Entity, bool) {
	for _, entity := range s.Entities {
		if entity.Name == name {
			return entity, true
		}
	}
	return Entity{}, false
}

// FieldNames returns the declared field names of an entity, in order.
func (e Entity) FieldNames() []string {
	names := make([]string, 0, len(e.Fields))
	for _, field := range e.Fields {
		names = append(names, field.Name)
	}
	return names
}

// Field returns the named field definition.
func (e Entity) Field(name string) (Field, bool) {
	for _, field := range e.Fields {
		if field.Name == name {
			return field, true
		}
	}
	return Field{}, false
}

// FieldCount returns how many fields an entity declares.
func (e Entity) FieldCount() int { return len(e.Fields) }

// ParseDate reads a date in the only accepted representation.
func ParseDate(value string) (time.Time, error) {
	return time.Parse(DateFormat, value)
}
