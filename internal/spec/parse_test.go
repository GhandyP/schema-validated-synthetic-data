package spec

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseRoundTripsTheReferenceSpec(t *testing.T) {
	want := baseSpec()

	encoded, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal reference spec: %v", err)
	}

	got, err := Parse(encoded)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Parse() =\n%+v\nwant\n%+v", got, want)
	}
}

func TestParseRefusesInputItCannotTrust(t *testing.T) {
	const impossible = `{"name":"x","seed":1,"entities":[{"name":"e","count":1,"fields":[{"name":"f","type":"boolean","unique":true}]}]}`

	tests := []struct {
		name    string
		input   string
		want    string
		partial bool
	}{
		{
			name:  "impossible spec is refused verbatim",
			input: impossible,
			want:  `field "e.f": unique is not supported for boolean fields; the domain holds two values`,
		},
		{
			name:    "unknown key is refused instead of ignored",
			input:   `{"name":"x","seed":1,"uniqe":true,"entities":[]}`,
			want:    `parse spec: json: unknown field "uniqe"`,
			partial: true,
		},
		{
			name:  "content after the object is refused",
			input: `{"name":"x","seed":1,"entities":[]} {"name":"y"}`,
			want:  "parse spec: unexpected content after the JSON object",
		},
		{
			name:    "malformed JSON is refused",
			input:   `{"name":"x",`,
			want:    "parse spec: unexpected EOF",
			partial: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.input))
			if err == nil {
				t.Fatalf("Parse() error = nil, want %q", tt.want)
			}

			got := err.Error()
			if tt.partial {
				if !strings.Contains(got, tt.want) {
					t.Fatalf("Parse() error = %q, want it to contain %q", got, tt.want)
				}
				return
			}
			if got != tt.want {
				t.Fatalf("Parse() error = %q, want %q", got, tt.want)
			}
			if tt.input == impossible {
				var problems Problems
				if !errors.As(err, &problems) {
					t.Fatalf("Parse() error = %v, want an unwrapped spec.Problems value", err)
				}
			}
		})
	}
}

func TestLoadNamesTheFileItRefused(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "impossible.json")

	const impossible = `{"name":"x","seed":1,"entities":[{"name":"e","count":1,"fields":[{"name":"f","type":"boolean","unique":true}]}]}`
	if err := os.WriteFile(path, []byte(impossible), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	_, err := Load(path)
	want := fmt.Sprintf(`%s: field "e.f": unique is not supported for boolean fields; the domain holds two values`, path)
	if err == nil || err.Error() != want {
		t.Fatalf("Load() error = %v, want %q", err, want)
	}

	missing := filepath.Join(dir, "missing.json")
	if _, err := Load(missing); err == nil || !strings.Contains(err.Error(), "read spec: ") {
		t.Fatalf("Load(missing) error = %v, want a read spec error", err)
	}
}

func TestEmptyProblemsIsANilError(t *testing.T) {
	empty := Problems{}
	if err := empty.OrNil(); err != nil {
		t.Fatalf("Problems{}.OrNil() error = %v, want nil", err)
	}

	reported := Problems{"one"}
	if err := reported.OrNil(); err == nil || err.Error() != "one" {
		t.Fatalf("Problems{one}.OrNil() error = %v, want %q", err, "one")
	}
}
