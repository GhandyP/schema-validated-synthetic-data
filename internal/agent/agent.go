// Package agent compiles a natural-language request into a dataset specification
// with Google ADK.
//
// This is the only package in the repository that calls a model, and the model
// has exactly one job: turn prose into a specification. It never produces a
// record, so it cannot break a rule that the generator has to keep. What it does
// produce is treated as untrusted input and passes through the same static rules
// as a hand-written spec file before anything is generated from it.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"

	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/model/gemini"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/genai"

	"github.com/GhandyP/schema-validated-synthetic-data/internal/spec"
)

// DefaultModel is used when ADK_MODEL is not set.
const DefaultModel = "gemini-flash-latest"

// Instruction is the stable instruction of the compiler agent. It restates the
// rules of internal/spec, because the model has to produce a specification that
// package accepts; when one of them changes, both change.
const Instruction = `You compile a request for synthetic data into a dataset specification.

You never write a record. Another program generates every record from the
specification you produce, so everything below is a rule about the shape of the
specification, never about values.

Field types and the keys each type requires:
- "string": needs "values" (a closed list) or "pattern", or both. Pattern tokens
  are {seq} for the record number, {seq:W} for that number zero-padded to W
  digits, and {word} for a word from a built-in synthetic list. Example:
  "USR-{seq:4}".
- "integer": needs "min" and "max", both whole numbers.
- "number": needs "min" and "max".
- "boolean": no extra keys.
- "date": needs "min_date" and "max_date" as YYYY-MM-DD.
- "enum": needs "values", a non-empty list of distinct strings.
- "reference": needs "references" as "entity.field", where the target field is
  unique. References must not form a cycle.

Rules:
- "count" must be greater than zero for every entity and every entity needs at
  least one field.
- "unique": true is accepted only where distinct values are provable: an integer
  range, a date range, an enum or value list, a pattern containing {seq}, or a
  reference to a unique field whose entity has at least as many records. It is
  never accepted for "number" or "boolean".
- Prefer "nullable": true over inventing a sentinel value for a missing case.
- A key that does not belong to its type is refused, including "min" on a date or
  "values" on an integer.
- Mark every column that resembles personal data with "sensitive": true.
- Never put real personal data in the specification: no real names, addresses,
  mailboxes, telephone numbers, account identifiers, or values copied from a real
  record. Where wording is needed, use neutral synthetic values.
- Record everything you could not express exactly in "assumptions", as short
  sentences a reader can act on, and leave "assumptions" out when there is
  nothing to say.
- Answer with the specification only. Never include records, sample rows, or
  commentary.`

// Compiler turns a request into a specification.
type Compiler interface {
	Compile(ctx context.Context, request string) (spec.DatasetSpec, error)
}

// ADKCompiler compiles requests through a Google ADK runner.
type ADKCompiler struct {
	runner         *runner.Runner
	sessionCounter atomic.Uint64
}

var _ Compiler = (*ADKCompiler)(nil)

// ErrMissingAPIKey reports that GOOGLE_API_KEY was not configured.
var ErrMissingAPIKey = errors.New("missing GOOGLE_API_KEY")

var errEmptyResponse = errors.New("the model returned no specification")

// NewADKCompiler builds a compiler around any model.LLM, which is what lets the
// tests exercise this package through a fake model instead of the network.
func NewADKCompiler(m model.LLM) (*ADKCompiler, error) {
	compilerAgent, err := llmagent.New(llmagent.Config{
		Name:        "spec_compiler",
		Description: "compiling a synthetic data request into a dataset specification",
		Model:       m,
		// The instruction teaches the {seq} and {word} pattern tokens by name. A plain
		// Instruction goes through ADK's session-state substitution, which resolves
		// anything shaped like {key_name} and fails the run before the model is
		// called; InstructionProvider is the documented way to keep those braces
		// literal.
		InstructionProvider: func(adkagent.ReadonlyContext) (string, error) { return Instruction, nil },
		OutputSchema:        OutputSchema(),
	})
	if err != nil {
		return nil, fmt.Errorf("create spec compiler agent: %w", err)
	}

	r, err := runner.NewInMemory("schema-validated-synthetic-data", compilerAgent)
	if err != nil {
		return nil, fmt.Errorf("create spec compiler runner: %w", err)
	}
	return &ADKCompiler{runner: r}, nil
}

// NewGeminiCompiler builds an ADK compiler backed by Gemini.
func NewGeminiCompiler(ctx context.Context) (*ADKCompiler, error) {
	key := strings.TrimSpace(os.Getenv("GOOGLE_API_KEY"))
	if key == "" {
		return nil, fmt.Errorf("create Gemini compiler: %w", ErrMissingAPIKey)
	}

	m, err := gemini.NewModel(ctx, ModelName(), &genai.ClientConfig{APIKey: key})
	if err != nil {
		return nil, fmt.Errorf("create Gemini model: %w", err)
	}
	return NewADKCompiler(m)
}

// ModelName reports the model the compiler will use.
func ModelName() string {
	if name := strings.TrimSpace(os.Getenv("ADK_MODEL")); name != "" {
		return name
	}
	return DefaultModel
}

// Prompt wraps a request in the compiling task.
func Prompt(request string) string {
	var b strings.Builder
	b.WriteString("Compile this request into a dataset specification.\n\nRequest:\n")
	b.WriteString(strings.TrimSpace(request))
	return b.String()
}

// Compile runs one ADK turn, decodes the structured answer, and applies the same
// static rules a hand-written specification has to pass.
//
// The seed is left at zero on purpose: determinism belongs to the caller that
// runs the batch, not to the model that describes it.
func (c *ADKCompiler) Compile(ctx context.Context, request string) (spec.DatasetSpec, error) {
	message := genai.NewContentFromText(Prompt(request), genai.RoleUser)
	sessionID := fmt.Sprintf("compile-%d", c.sessionCounter.Add(1))

	var responseText string
	for event, err := range c.runner.Run(ctx, "user", sessionID, message, adkagent.RunConfig{}) {
		if err != nil {
			return spec.DatasetSpec{}, fmt.Errorf("run spec compiler: %w", err)
		}
		if event == nil || event.Content == nil {
			continue
		}
		responseText = contentText(event.Content)
	}

	if strings.TrimSpace(responseText) == "" {
		return spec.DatasetSpec{}, fmt.Errorf("run spec compiler: %w", errEmptyResponse)
	}

	var compiled spec.DatasetSpec
	if err := json.Unmarshal([]byte(responseText), &compiled); err != nil {
		return spec.DatasetSpec{}, fmt.Errorf("decode compiled spec: %w", err)
	}
	compiled.Seed = 0

	if err := compiled.Validate(); err != nil {
		return spec.DatasetSpec{}, fmt.Errorf("compiled spec is not usable: %w", err)
	}
	return compiled, nil
}

func contentText(content *genai.Content) string {
	var b strings.Builder
	for _, part := range content.Parts {
		if part != nil {
			b.WriteString(part.Text)
		}
	}
	return b.String()
}
