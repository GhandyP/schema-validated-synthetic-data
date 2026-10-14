// Package cli wires the dry-run pipeline behind one command: read or compile a
// specification, generate the batch, re-check every declared rule, and print.
//
// It never writes a file, loads a database, or reads a data source. The only
// thing it reads from disk is the specification the caller named.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/GhandyP/schema-validated-synthetic-data/internal/agent"
	"github.com/GhandyP/schema-validated-synthetic-data/internal/generate"
	"github.com/GhandyP/schema-validated-synthetic-data/internal/spec"
	"github.com/GhandyP/schema-validated-synthetic-data/internal/validate"
)

// Exit codes. They are part of the command's contract and are documented in the
// README, because a caller scripting this command has to tell a bad request apart
// from a defect in this repository.
const (
	// ExitOK means the batch was generated and every declared rule held.
	ExitOK = 0
	// ExitUsage means the command line itself was wrong.
	ExitUsage = 2
	// ExitRefused means the specification, written or compiled, was refused.
	ExitRefused = 3
	// ExitModel means the model could not be used to compile a request.
	ExitModel = 4
	// ExitViolation means the validator found a rule the generator broke. The
	// generator is built to make that impossible, so this code reports a defect
	// in this repository rather than a mistake by the caller.
	ExitViolation = 5
)

// Deps carries the collaborators a run needs, so a caller can replace them.
type Deps struct {
	// NewCompiler builds the compiler used by -doc. It is called only when a
	// plain-language request has to be compiled, so a -spec run needs no key and
	// no network. A nil value makes -doc an error.
	NewCompiler func(ctx context.Context) (agent.Compiler, error)
	// Validate re-checks the batch. A nil value uses validate.Dataset. It is a
	// field so the defect path can be exercised without corrupting the generator.
	Validate func(s spec.DatasetSpec, dataset spec.SyntheticDataset) spec.ValidationReport
}

// Run parses the command line, runs the pipeline, prints the result, and returns
// the process exit code.
func Run(ctx context.Context, args []string, deps Deps, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("schema-validated-synthetic-data", flag.ContinueOnError)
	flags.SetOutput(stderr)

	specPath := flags.String("spec", "", "path to a dataset specification in JSON; runs the whole pipeline without a model")
	doc := flags.String("doc", "", "plain-language request for the model to compile into a specification")
	seed := flags.Int64("seed", 0, "seed of the batch, overriding the seed declared in the specification")
	asJSON := flags.Bool("json", false, "print the dataset as JSON instead of the preview")

	flags.Usage = func() { usage(stderr, flags) }

	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return ExitOK
		}
		return ExitUsage
	}

	seedGiven := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "seed" {
			seedGiven = true
		}
	})

	switch {
	case flags.NArg() > 0:
		fmt.Fprintf(stderr, "unexpected argument %q\n\n", flags.Arg(0))
		usage(stderr, flags)
		return ExitUsage
	case *specPath == "" && *doc == "":
		fmt.Fprint(stderr, "give -spec to read a specification, or -doc to compile a request\n\n")
		usage(stderr, flags)
		return ExitUsage
	case *specPath != "" && *doc != "":
		fmt.Fprint(stderr, "-spec and -doc are mutually exclusive\n\n")
		usage(stderr, flags)
		return ExitUsage
	}

	declared, code := specification(ctx, *specPath, *doc, deps, stderr)
	if code != ExitOK {
		return code
	}
	if seedGiven {
		declared.Seed = *seed
	}

	dataset, err := generate.Records(declared)
	if err != nil {
		failure(stderr, "the specification was refused", err)
		return ExitRefused
	}

	if *doc != "" {
		dataset.Assumptions = append(dataset.Assumptions,
			fmt.Sprintf("the specification was compiled with %s from a plain-language request", agent.ModelName()))
	}

	report := validateWith(deps, declared, dataset)
	dataset.Validation = &report
	if !report.Passed {
		printViolations(stderr, report)
		return ExitViolation
	}

	if *asJSON {
		encoded, err := json.MarshalIndent(dataset, "", "  ")
		if err != nil {
			failure(stderr, "the dataset could not be encoded", err)
			return ExitViolation
		}
		fmt.Fprintf(stdout, "%s\n", encoded)
		return ExitOK
	}

	preview(stdout, dataset)
	return ExitOK
}

// specification reads or compiles the request, reporting the exit code of a
// failure.
func specification(ctx context.Context, specPath, doc string, deps Deps, stderr io.Writer) (spec.DatasetSpec, int) {
	if specPath != "" {
		loaded, err := spec.Load(specPath)
		if err != nil {
			failure(stderr, "the specification was refused", err)
			return spec.DatasetSpec{}, ExitRefused
		}
		return loaded, ExitOK
	}

	if deps.NewCompiler == nil {
		failure(stderr, "the model could not be used", errors.New("no compiler is configured"))
		return spec.DatasetSpec{}, ExitModel
	}
	compiler, err := deps.NewCompiler(ctx)
	if err != nil {
		failure(stderr, "the model could not be used", err)
		return spec.DatasetSpec{}, ExitModel
	}

	compiled, err := compiler.Compile(ctx, doc)
	if err != nil {
		// An answer the contract refuses is the caller's problem to fix; a model
		// that could not answer at all is an environment problem. The two exit
		// codes keep them apart.
		var problems spec.Problems
		if errors.As(err, &problems) {
			failure(stderr, "the compiled specification was refused", err)
			return spec.DatasetSpec{}, ExitRefused
		}
		failure(stderr, "the model could not be used", err)
		return spec.DatasetSpec{}, ExitModel
	}
	return compiled, ExitOK
}

func validateWith(deps Deps, declared spec.DatasetSpec, dataset spec.SyntheticDataset) spec.ValidationReport {
	if deps.Validate != nil {
		return deps.Validate(declared, dataset)
	}
	return validate.Dataset(declared, dataset)
}

// failure prints an error, one problem per line when the error carries several.
func failure(stderr io.Writer, headline string, err error) {
	var problems spec.Problems
	if errors.As(err, &problems) {
		fmt.Fprintf(stderr, "%s:\n", headline)
		for _, problem := range problems {
			fmt.Fprintf(stderr, "  %s\n", problem)
		}
		return
	}
	fmt.Fprintf(stderr, "%s: %v\n", headline, err)
}

// printViolations reports the checks a defect broke, and nothing else: the point
// of this path is that a caller sees exactly which promise failed.
func printViolations(stderr io.Writer, report spec.ValidationReport) {
	fmt.Fprintf(stderr, "the batch broke %d of %d declared rules:\n", countFailures(report), report.Total)
	for _, check := range report.Checked {
		if check.Passed {
			continue
		}
		fmt.Fprintf(stderr, "  %s %s: %s\n", check.Rule, check.Subject, check.Detail)
	}
	fmt.Fprintln(stderr, "this is a defect in this repository, not in the specification")
}

func countFailures(report spec.ValidationReport) int {
	failed := 0
	for _, check := range report.Checked {
		if !check.Passed {
			failed++
		}
	}
	return failed
}

func usage(stderr io.Writer, flags *flag.FlagSet) {
	fmt.Fprint(stderr, "usage: schema-validated-synthetic-data (-spec <file> | -doc <request>) [options]\n\n")
	fmt.Fprintf(stderr, "Dry run: the batch is generated in memory and printed. Nothing is written,\nand no data source is read. Records are synthetic; every declared rule is\nre-checked before anything is printed.\n\n")
	flags.PrintDefaults()
	fmt.Fprintf(stderr, "\nexit codes: %d every rule held, %d usage, %d specification refused, %d model unusable, %d validation found a violation\n",
		ExitOK, ExitUsage, ExitRefused, ExitModel, ExitViolation)
}
