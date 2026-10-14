package main

import (
	"fmt"
	"os"
)

// main is replaced by the real entry point once the pipeline lands; until then it
// reports that the repository is under construction instead of pretending to run.
func main() {
	fmt.Fprintln(os.Stderr, "schema-validated-synthetic-data is under construction; see odd/tasks/schema-validated-synthetic-data.md")
	os.Exit(1)
}
