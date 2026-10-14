package agent

import (
	"google.golang.org/genai"

	"github.com/GhandyP/schema-validated-synthetic-data/internal/spec"
)

// OutputSchema is the structured schema the compiler agent must answer with.
//
// It mirrors spec.DatasetSpec without the seed field: the model describes the
// batch, and the caller decides the seed. A model that answers outside this
// schema cannot reach the generator, and a model that answers inside it still
// passes through spec.Validate before any record exists.
func OutputSchema() *genai.Schema {
	return &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"name": {
				Type:        genai.TypeString,
				Description: "Short name of the batch, for example \"support_tickets\".",
			},
			"entities": {
				Type:        genai.TypeArray,
				Description: "The tables to generate. One entity for a flat batch.",
				Items:       entitySchema(),
			},
			"assumptions": {
				Type:        genai.TypeArray,
				Description: "What could not be expressed exactly in this specification.",
				Items:       &genai.Schema{Type: genai.TypeString},
			},
		},
		Required: []string{"name", "entities"},
	}
}

func entitySchema() *genai.Schema {
	return &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"name": {
				Type:        genai.TypeString,
				Description: "Table name, used as the prefix of \"entity.field\" references.",
			},
			"count": {
				Type:        genai.TypeInteger,
				Description: "How many records to generate. Must be greater than zero.",
			},
			"fields": {
				Type:  genai.TypeArray,
				Items: fieldSchema(),
			},
		},
		Required: []string{"name", "count", "fields"},
	}
}

func fieldSchema() *genai.Schema {
	return &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"name": {
				Type:        genai.TypeString,
				Description: "Column name.",
			},
			"type": {
				Type:        genai.TypeString,
				Format:      "enum",
				Enum:        spec.FieldTypes(),
				Description: "Value domain. Each type requires the keys named in the instruction.",
			},
			"unique": {
				Type:        genai.TypeBoolean,
				Description: "Whether values must be distinct. Only accepted where distinct values are provable.",
			},
			"nullable": {
				Type:        genai.TypeBoolean,
				Description: "Whether null is allowed in place of a value.",
			},
			"sensitive": {
				Type:        genai.TypeBoolean,
				Description: "Whether the column resembles personal data. Its values are still generated, never sampled.",
			},
			"min": {
				Type:        genai.TypeNumber,
				Description: "Lower bound of an integer or number field.",
			},
			"max": {
				Type:        genai.TypeNumber,
				Description: "Upper bound of an integer or number field.",
			},
			"min_date": {
				Type:        genai.TypeString,
				Description: "Lower bound of a date field, as YYYY-MM-DD.",
			},
			"max_date": {
				Type:        genai.TypeString,
				Description: "Upper bound of a date field, as YYYY-MM-DD.",
			},
			"values": {
				Type:        genai.TypeArray,
				Description: "Closed domain of an enum field, or of a string field.",
				Items:       &genai.Schema{Type: genai.TypeString},
			},
			"pattern": {
				Type:        genai.TypeString,
				Description: "Template of a string field, using {seq}, {seq:W} or {word}.",
			},
			"references": {
				Type:        genai.TypeString,
				Description: "Target of a reference field, as \"entity.field\", pointing at a unique field.",
			},
		},
		Required: []string{"name", "type"},
	}
}
