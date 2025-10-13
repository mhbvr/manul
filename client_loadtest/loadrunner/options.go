package loadrunner

import (
	"fmt"
	"reflect"
	"strconv"
)

// ParseOptions parses a map[string]string into a struct using field tags.
// Supported tags:
//   - `name:"option_name"` - the name of the option in the map
//   - `description:"option description"` - documentation for the option
//
// Supported types: string, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, bool, float32, float64
func ParseOptions(options map[string]string, target interface{}) error {
	v := reflect.ValueOf(target)
	if v.Kind() != reflect.Ptr {
		return fmt.Errorf("target must be a pointer to a struct")
	}

	v = v.Elem()
	if v.Kind() != reflect.Struct {
		return fmt.Errorf("target must be a pointer to a struct")
	}

	t := v.Type()

	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		fieldType := t.Field(i)

		// Get the option name from the tag
		optionName := fieldType.Tag.Get("name")
		if optionName == "" {
			continue // Skip fields without a name tag
		}

		// Get the value from the options map
		value, ok := options[optionName]
		if !ok {
			continue // Skip if option not provided
		}

		// Set the field value based on its type
		if !field.CanSet() {
			return fmt.Errorf("cannot set field %s", fieldType.Name)
		}

		if err := setField(field, value); err != nil {
			return fmt.Errorf("error setting field %s: %w", fieldType.Name, err)
		}
	}

	return nil
}

func setField(field reflect.Value, value string) error {
	switch field.Kind() {
	case reflect.String:
		field.SetString(value)

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		intVal, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid int value: %w", err)
		}
		field.SetInt(intVal)

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		uintVal, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid uint value: %w", err)
		}
		field.SetUint(uintVal)

	case reflect.Bool:
		boolVal, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("invalid bool value: %w", err)
		}
		field.SetBool(boolVal)

	case reflect.Float32, reflect.Float64:
		floatVal, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return fmt.Errorf("invalid float value: %w", err)
		}
		field.SetFloat(floatVal)

	default:
		return fmt.Errorf("unsupported field type: %s", field.Kind())
	}

	return nil
}

// GetOptionDesc returns a list of option descriptions
// by inspecting the struct tags and current values.
func GetOptionsDesc(target interface{}) map[string]string {
	res := make(map[string]string)

	v := reflect.ValueOf(target)
	t := reflect.TypeOf(target)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
		v = v.Elem()
	}

	if t.Kind() != reflect.Struct {
		return res
	}

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		optionName := field.Tag.Get("name")
		if optionName == "" {
			continue
		}

		description := field.Tag.Get("description")
		res[optionName] = description
	}

	return res
}
