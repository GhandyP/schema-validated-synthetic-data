package agent

import (
	"context"

	"github.com/GhandyP/schema-validated-synthetic-data/internal/spec"
)

// Fake is a Compiler that answers with a fixed result.
//
// It exists so the rest of the pipeline can be exercised, by tests and by a
// caller who already has a specification, without a model or an API key: the
// compilation step is the only part of this repository that needs either.
type Fake struct {
	// Spec is the specification the fake returns.
	Spec spec.DatasetSpec
	// Err, when set, is returned instead of Spec.
	Err error
}

var _ Compiler = Fake{}

// Compile returns the fixed result and ignores the request.
func (f Fake) Compile(context.Context, string) (spec.DatasetSpec, error) {
	if f.Err != nil {
		return spec.DatasetSpec{}, f.Err
	}
	return f.Spec, nil
}
