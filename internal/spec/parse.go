package spec

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

// Parse decodes a specification and refuses it unless every static rule holds.
//
// Decoding is strict: an unknown key is an error, so a typo such as "uniqe" fails
// loudly instead of quietly producing values that are not unique. A refusal
// happens before any record is generated, which is the point of this package.
func Parse(data []byte) (DatasetSpec, error) {
	var s DatasetSpec

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&s); err != nil {
		return DatasetSpec{}, fmt.Errorf("parse spec: %w", err)
	}

	if err := decoder.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		return DatasetSpec{}, errors.New("parse spec: unexpected content after the JSON object")
	}

	if err := s.Validate(); err != nil {
		return DatasetSpec{}, err
	}
	return s, nil
}

// Load reads a specification file and parses it, naming the file in the error.
func Load(path string) (DatasetSpec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return DatasetSpec{}, fmt.Errorf("read spec: %w", err)
	}

	s, err := Parse(data)
	if err != nil {
		return DatasetSpec{}, fmt.Errorf("%s: %w", path, err)
	}
	return s, nil
}
