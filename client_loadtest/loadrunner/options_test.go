package loadrunner

import (
	"testing"
)

type TestOptions struct {
	StringField  string  `name:"string_opt" description:"A string option"`
	IntField     int     `name:"int_opt" description:"An int option"`
	Int64Field   int64   `name:"int64_opt" description:"An int64 option"`
	UintField    uint    `name:"uint_opt" description:"A uint option"`
	Uint64Field  uint64  `name:"uint64_opt" description:"A uint64 option"`
	BoolField    bool    `name:"bool_opt" description:"A bool option"`
	Float32Field float32 `name:"float32_opt" description:"A float32 option"`
	Float64Field float64 `name:"float64_opt" description:"A float64 option"`
	NoTagField   string  // Field without tag should be ignored
}

func TestParseOptions_AllTypes(t *testing.T) {
	options := map[string]string{
		"string_opt":  "test_value",
		"int_opt":     "42",
		"int64_opt":   "9223372036854775807",
		"uint_opt":    "123",
		"uint64_opt":  "18446744073709551615",
		"bool_opt":    "true",
		"float32_opt": "3.14",
		"float64_opt": "2.718281828",
	}

	var target TestOptions
	err := ParseOptions(options, &target)
	if err != nil {
		t.Fatalf("ParseOptions failed: %v", err)
	}

	if target.StringField != "test_value" {
		t.Errorf("StringField: expected 'test_value', got '%s'", target.StringField)
	}
	if target.IntField != 42 {
		t.Errorf("IntField: expected 42, got %d", target.IntField)
	}
	if target.Int64Field != 9223372036854775807 {
		t.Errorf("Int64Field: expected 9223372036854775807, got %d", target.Int64Field)
	}
	if target.UintField != 123 {
		t.Errorf("UintField: expected 123, got %d", target.UintField)
	}
	if target.Uint64Field != 18446744073709551615 {
		t.Errorf("Uint64Field: expected 18446744073709551615, got %d", target.Uint64Field)
	}
	if target.BoolField != true {
		t.Errorf("BoolField: expected true, got %t", target.BoolField)
	}
	if target.Float32Field != 3.14 {
		t.Errorf("Float32Field: expected 3.14, got %f", target.Float32Field)
	}
	if target.Float64Field != 2.718281828 {
		t.Errorf("Float64Field: expected 2.718281828, got %f", target.Float64Field)
	}
}

func TestParseOptions_PartialOptions(t *testing.T) {
	options := map[string]string{
		"string_opt": "partial",
		"int_opt":    "10",
	}

	var target TestOptions
	err := ParseOptions(options, &target)
	if err != nil {
		t.Fatalf("ParseOptions failed: %v", err)
	}

	if target.StringField != "partial" {
		t.Errorf("StringField: expected 'partial', got '%s'", target.StringField)
	}
	if target.IntField != 10 {
		t.Errorf("IntField: expected 10, got %d", target.IntField)
	}
	// Other fields should have zero values
	if target.BoolField != false {
		t.Errorf("BoolField: expected false, got %t", target.BoolField)
	}
	if target.Float64Field != 0 {
		t.Errorf("Float64Field: expected 0, got %f", target.Float64Field)
	}
}

func TestParseOptions_InvalidInt(t *testing.T) {
	options := map[string]string{
		"int_opt": "not_a_number",
	}

	var target TestOptions
	err := ParseOptions(options, &target)
	if err == nil {
		t.Fatal("Expected error for invalid int, got nil")
	}
}

func TestParseOptions_InvalidBool(t *testing.T) {
	options := map[string]string{
		"bool_opt": "not_a_bool",
	}

	var target TestOptions
	err := ParseOptions(options, &target)
	if err == nil {
		t.Fatal("Expected error for invalid bool, got nil")
	}
}

func TestParseOptions_InvalidFloat(t *testing.T) {
	options := map[string]string{
		"float64_opt": "not_a_float",
	}

	var target TestOptions
	err := ParseOptions(options, &target)
	if err == nil {
		t.Fatal("Expected error for invalid float, got nil")
	}
}

func TestParseOptions_NotPointer(t *testing.T) {
	options := map[string]string{
		"string_opt": "test",
	}

	var target TestOptions
	err := ParseOptions(options, target) // Not passing pointer
	if err == nil {
		t.Fatal("Expected error for non-pointer target, got nil")
	}
}

func TestParseOptions_NilPointer(t *testing.T) {
	options := map[string]string{
		"string_opt": "test",
	}

	var target *TestOptions
	err := ParseOptions(options, target)
	if err == nil {
		t.Fatal("Expected error for nil pointer, got nil")
	}
}

func TestParseOptions_EmptyMap(t *testing.T) {
	options := map[string]string{}

	var target TestOptions
	err := ParseOptions(options, &target)
	if err != nil {
		t.Fatalf("ParseOptions failed with empty map: %v", err)
	}

	// All fields should have zero values
	if target.StringField != "" {
		t.Errorf("StringField: expected empty string, got '%s'", target.StringField)
	}
	if target.IntField != 0 {
		t.Errorf("IntField: expected 0, got %d", target.IntField)
	}
}

func TestGetOptionDescriptions(t *testing.T) {
	target := TestOptions{
		StringField:  "default_string",
		IntField:     100,
		Int64Field:   200,
		UintField:    300,
		Uint64Field:  400,
		BoolField:    true,
		Float32Field: 1.5,
		Float64Field: 2.5,
	}
	descriptions := GetOptionDescriptions(&target)

	expected := map[string]struct {
		description  string
		defaultValue string
	}{
		"string_opt":  {"A string option", "default_string"},
		"int_opt":     {"An int option", "100"},
		"int64_opt":   {"An int64 option", "200"},
		"uint_opt":    {"A uint option", "300"},
		"uint64_opt":  {"A uint64 option", "400"},
		"bool_opt":    {"A bool option", "true"},
		"float32_opt": {"A float32 option", "1.5"},
		"float64_opt": {"A float64 option", "2.5"},
	}

	if len(descriptions) != len(expected) {
		t.Errorf("Expected %d descriptions, got %d", len(expected), len(descriptions))
	}

	for _, desc := range descriptions {
		exp, ok := expected[desc.Name]
		if !ok {
			t.Errorf("Unexpected option '%s'", desc.Name)
			continue
		}
		if desc.Description != exp.description {
			t.Errorf("Option '%s': expected description '%s', got '%s'", desc.Name, exp.description, desc.Description)
		}
		if desc.DefaultValue != exp.defaultValue {
			t.Errorf("Option '%s': expected default value '%s', got '%s'", desc.Name, exp.defaultValue, desc.DefaultValue)
		}
	}
}

func TestGetOptionDescriptions_ZeroValues(t *testing.T) {
	var target TestOptions
	descriptions := GetOptionDescriptions(&target)

	for _, desc := range descriptions {
		switch desc.Name {
		case "string_opt":
			if desc.DefaultValue != "" {
				t.Errorf("Expected empty string for string_opt, got '%s'", desc.DefaultValue)
			}
		case "int_opt", "int64_opt", "uint_opt", "uint64_opt":
			if desc.DefaultValue != "0" {
				t.Errorf("Expected '0' for %s, got '%s'", desc.Name, desc.DefaultValue)
			}
		case "bool_opt":
			if desc.DefaultValue != "false" {
				t.Errorf("Expected 'false' for bool_opt, got '%s'", desc.DefaultValue)
			}
		case "float32_opt", "float64_opt":
			if desc.DefaultValue != "0" {
				t.Errorf("Expected '0' for %s, got '%s'", desc.Name, desc.DefaultValue)
			}
		}
	}
}

func TestGetOptionDescriptions_NonStruct(t *testing.T) {
	var notStruct int
	descriptions := GetOptionDescriptions(&notStruct)

	if len(descriptions) != 0 {
		t.Errorf("Expected empty list for non-struct, got %d entries", len(descriptions))
	}
}

func TestParseOptions_BoolVariants(t *testing.T) {
	testCases := []struct {
		value    string
		expected bool
	}{
		{"true", true},
		{"True", true},
		{"TRUE", true},
		{"1", true},
		{"false", false},
		{"False", false},
		{"FALSE", false},
		{"0", false},
	}

	for _, tc := range testCases {
		options := map[string]string{
			"bool_opt": tc.value,
		}

		var target TestOptions
		err := ParseOptions(options, &target)
		if err != nil {
			t.Fatalf("ParseOptions failed for bool value '%s': %v", tc.value, err)
		}

		if target.BoolField != tc.expected {
			t.Errorf("Bool value '%s': expected %t, got %t", tc.value, tc.expected, target.BoolField)
		}
	}
}

func TestParseOptions_NegativeNumbers(t *testing.T) {
	options := map[string]string{
		"int_opt":     "-42",
		"int64_opt":   "-9223372036854775808",
		"float64_opt": "-3.14",
	}

	var target TestOptions
	err := ParseOptions(options, &target)
	if err != nil {
		t.Fatalf("ParseOptions failed: %v", err)
	}

	if target.IntField != -42 {
		t.Errorf("IntField: expected -42, got %d", target.IntField)
	}
	if target.Int64Field != -9223372036854775808 {
		t.Errorf("Int64Field: expected -9223372036854775808, got %d", target.Int64Field)
	}
	if target.Float64Field != -3.14 {
		t.Errorf("Float64Field: expected -3.14, got %f", target.Float64Field)
	}
}

func TestParseOptions_NegativeUint(t *testing.T) {
	options := map[string]string{
		"uint_opt": "-1",
	}

	var target TestOptions
	err := ParseOptions(options, &target)
	if err == nil {
		t.Fatal("Expected error for negative uint, got nil")
	}
}
