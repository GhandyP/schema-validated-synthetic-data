package agent

import (
	"context"
	"errors"
	"iter"
	"reflect"
	"slices"
	"strings"
	"testing"

	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"

	"github.com/GhandyP/schema-validated-synthetic-data/internal/spec"
)

func number(value float64) *float64 { return &value }

// validResponse is a well-formed answer from the compiler agent.
const validResponse = `{
  "name": "support_tickets",
  "entities": [
    {
      "name": "customer",
      "count": 3,
      "fields": [
        {"name": "id", "type": "string", "unique": true, "pattern": "C-{seq:4}"},
        {"name": "tier", "type": "enum", "values": ["free", "pro", "enterprise"]},
        {"name": "age", "type": "integer", "min": 18, "max": 90}
      ]
    },
    {
      "name": "ticket",
      "count": 5,
      "fields": [
        {"name": "id", "type": "string", "unique": true, "pattern": "T-{seq}"},
        {"name": "customer_id", "type": "reference", "references": "customer.id"},
        {"name": "opened_at", "type": "date", "min_date": "2026-01-01", "max_date": "2026-09-30"}
      ]
    }
  ],
  "assumptions": ["the request did not state how many customers exist"]
}`

type fakeModel struct {
	response string
	err      error
	requests []*model.LLMRequest
}

func (m *fakeModel) Name() string { return "fake-model" }

func (m *fakeModel) GenerateContent(_ context.Context, req *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
	m.requests = append(m.requests, req)

	return func(yield func(*model.LLMResponse, error) bool) {
		if m.err != nil {
			yield(nil, m.err)
			return
		}
		yield(&model.LLMResponse{
			Content:      genai.NewContentFromText(m.response, genai.RoleModel),
			TurnComplete: true,
		}, nil)
	}
}

func newTestCompiler(t *testing.T, m model.LLM) *ADKCompiler {
	t.Helper()

	compiler, err := NewADKCompiler(m)
	if err != nil {
		t.Fatalf("NewADKCompiler() error = %v, want nil", err)
	}
	return compiler
}

func requestText(req *model.LLMRequest) string {
	var b strings.Builder
	for _, content := range req.Contents {
		b.WriteString(contentText(content))
	}
	return b.String()
}

func TestCompileDecodesTheStructuredSpec(t *testing.T) {
	compiled, err := newTestCompiler(t, &fakeModel{response: validResponse}).Compile(context.Background(), "Support tickets, fictional.")
	if err != nil {
		t.Fatalf("Compile() error = %v, want nil", err)
	}

	want := spec.DatasetSpec{
		Name: "support_tickets",
		Entities: []spec.Entity{
			{
				Name:  "customer",
				Count: 3,
				Fields: []spec.Field{
					{Name: "id", Type: spec.TypeString, Unique: true, Pattern: "C-{seq:4}"},
					{Name: "tier", Type: spec.TypeEnum, Values: []string{"free", "pro", "enterprise"}},
					{Name: "age", Type: spec.TypeInteger, Min: number(18), Max: number(90)},
				},
			},
			{
				Name:  "ticket",
				Count: 5,
				Fields: []spec.Field{
					{Name: "id", Type: spec.TypeString, Unique: true, Pattern: "T-{seq}"},
					{Name: "customer_id", Type: spec.TypeReference, References: "customer.id"},
					{Name: "opened_at", Type: spec.TypeDate, MinDate: "2026-01-01", MaxDate: "2026-09-30"},
				},
			},
		},
		Assumptions: []string{"the request did not state how many customers exist"},
	}

	if !reflect.DeepEqual(compiled, want) {
		t.Errorf("Compile() =\n%+v\nwant\n%+v", compiled, want)
	}
}

func TestTheInstructionReachesTheModelWithItsBracesIntact(t *testing.T) {
	m := &fakeModel{response: validResponse}

	if _, err := newTestCompiler(t, m).Compile(context.Background(), "anything"); err != nil {
		t.Fatalf("Compile() error = %v, want nil", err)
	}
	if len(m.requests) != 1 {
		t.Fatalf("model received %d requests, want exactly 1", len(m.requests))
	}
	if m.requests[0].Config == nil || m.requests[0].Config.SystemInstruction == nil {
		t.Fatal("the model received no system instruction")
	}

	// A plain llmagent.Config.Instruction would be templated against session state
	// and would fail on {seq} and {word} before the model is called, so the tokens
	// have to arrive verbatim.
	got := contentText(m.requests[0].Config.SystemInstruction)
	for _, want := range []string{"{seq}", "{seq:W}", "{word}"} {
		if !strings.Contains(got, want) {
			t.Errorf("the instruction the model received does not contain %q verbatim", want)
		}
	}
}

func TestCompileCarriesTheRequestToTheModel(t *testing.T) {
	m := &fakeModel{response: validResponse}
	compiler := newTestCompiler(t, m)

	if _, err := compiler.Compile(context.Background(), "Three fictional customers, please."); err != nil {
		t.Fatalf("Compile() error = %v, want nil", err)
	}

	if len(m.requests) != 1 {
		t.Fatalf("model received %d requests, want exactly 1", len(m.requests))
	}

	got := requestText(m.requests[0])
	for _, want := range []string{"Compile this request into a dataset specification.", "Three fictional customers, please."} {
		if !strings.Contains(got, want) {
			t.Errorf("request = %q, want it to contain %q", got, want)
		}
	}
}

func TestCompileLeavesTheSeedToTheCaller(t *testing.T) {
	const withSeed = `{"name":"x","seed":999,"entities":[{"name":"e","count":1,"fields":[{"name":"t","type":"boolean"}]}]}`

	compiled, err := newTestCompiler(t, &fakeModel{response: withSeed}).Compile(context.Background(), "anything")
	if err != nil {
		t.Fatalf("Compile() error = %v, want nil", err)
	}
	if compiled.Seed != 0 {
		t.Errorf("Compile().Seed = %d, want 0: the model must not decide the seed", compiled.Seed)
	}
}

func TestCompileRefusesAnAnswerThatBreaksTheStaticRules(t *testing.T) {
	const impossible = `{"name":"x","entities":[{"name":"e","count":1,"fields":[{"name":"ok","type":"boolean","unique":true}]}]}`

	_, err := newTestCompiler(t, &fakeModel{response: impossible}).Compile(context.Background(), "anything")
	if err == nil {
		t.Fatal("Compile() error = nil, want a refusal")
	}

	want := `compiled spec is not usable: field "e.ok": unique is not supported for boolean fields; the domain holds two values`
	if err.Error() != want {
		t.Errorf("Compile() error = %q, want %q", err, want)
	}

	var problems spec.Problems
	if !errors.As(err, &problems) {
		t.Errorf("Compile() error = %v, want an inspectable spec.Problems value", err)
	}
}

func TestCompileRefusesAnAnswerItCannotUse(t *testing.T) {
	tests := []struct {
		name     string
		response string
		modelErr error
		want     string
	}{
		{
			name:     "the model fails",
			modelErr: errors.New("quota exceeded"),
			want:     "run spec compiler: quota exceeded",
		},
		{
			name:     "no answer",
			response: "   ",
			want:     "run spec compiler: the model returned no specification",
		},
		{
			name:     "an answer that is not a specification",
			response: "Here is your specification: {…}",
			want:     "decode compiled spec: ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := newTestCompiler(t, &fakeModel{response: tt.response, err: tt.modelErr}).Compile(context.Background(), "anything")
			if err == nil {
				t.Fatalf("Compile() error = nil, want %q", tt.want)
			}

			got := err.Error()
			if strings.HasSuffix(tt.want, ": ") {
				if !strings.HasPrefix(got, tt.want) {
					t.Errorf("Compile() error = %q, want it to start with %q", got, tt.want)
				}
				return
			}
			if got != tt.want {
				t.Errorf("Compile() error = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestInstructionStatesTheContractTheValidatorEnforces(t *testing.T) {
	for _, want := range []string{
		"You never write a record.",
		"{seq:4}",
		"Never put real personal data in the specification",
		"assumptions",
		"nullable",
		"sensitive",
	} {
		if !strings.Contains(Instruction, want) {
			t.Errorf("Instruction does not mention %q", want)
		}
	}

	for _, fieldType := range spec.FieldTypes() {
		if !strings.Contains(Instruction, `"`+fieldType+`"`) {
			t.Errorf("Instruction does not describe the accepted type %q", fieldType)
		}
	}
}

func TestOutputSchemaOffersExactlyTheAcceptedTypesAndNoSeed(t *testing.T) {
	schema := OutputSchema()

	if !slices.Equal(schema.Required, []string{"name", "entities"}) {
		t.Errorf("required keys = %v, want the batch name and its entities", schema.Required)
	}
	if _, ok := schema.Properties["seed"]; ok {
		t.Error("the output schema offers a seed field, want none: the seed belongs to the caller")
	}

	field := schema.Properties["entities"].Items.Properties["fields"].Items
	if got := field.Properties["type"].Enum; !slices.Equal(got, spec.FieldTypes()) {
		t.Errorf("field type enum = %v, want the accepted types %v", got, spec.FieldTypes())
	}
	if !slices.Equal(field.Required, []string{"name", "type"}) {
		t.Errorf("required field keys = %v, want name and type", field.Required)
	}

	for _, key := range []string{"unique", "nullable", "sensitive", "min", "max", "min_date", "max_date", "values", "pattern", "references"} {
		if _, ok := field.Properties[key]; !ok {
			t.Errorf("the field schema is missing the %q key the contract accepts", key)
		}
	}
}

func TestFakeCompilerAnswersWithoutAModel(t *testing.T) {
	want := spec.DatasetSpec{Name: "handwritten", Entities: []spec.Entity{{Name: "e", Count: 1, Fields: []spec.Field{{Name: "t", Type: spec.TypeBoolean}}}}}

	compiled, err := Fake{Spec: want}.Compile(context.Background(), "ignored")
	if err != nil {
		t.Fatalf("Fake.Compile() error = %v, want nil", err)
	}
	if !reflect.DeepEqual(compiled, want) {
		t.Errorf("Fake.Compile() = %+v, want %+v", compiled, want)
	}

	failure := errors.New("no model available")
	if _, err := (Fake{Err: failure}).Compile(context.Background(), "ignored"); !errors.Is(err, failure) {
		t.Errorf("Fake.Compile() error = %v, want %v", err, failure)
	}
}

func TestModelNameHonoursTheEnvironment(t *testing.T) {
	t.Setenv("ADK_MODEL", "  ")
	if got := ModelName(); got != DefaultModel {
		t.Errorf("ModelName() = %q, want the default %q", got, DefaultModel)
	}

	t.Setenv("ADK_MODEL", "gemini-test-model")
	if got := ModelName(); got != "gemini-test-model" {
		t.Errorf("ModelName() = %q, want the configured model", got)
	}
}

func TestNewGeminiCompilerNeedsAKey(t *testing.T) {
	t.Setenv("GOOGLE_API_KEY", "")

	if _, err := NewGeminiCompiler(context.Background()); !errors.Is(err, ErrMissingAPIKey) {
		t.Errorf("NewGeminiCompiler() error = %v, want ErrMissingAPIKey", err)
	}
}
