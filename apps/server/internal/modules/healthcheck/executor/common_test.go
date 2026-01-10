package executor

import (
	"vigi/internal/utils"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Test the global validator instance
func TestGlobalValidator(t *testing.T) {
	// Test that the global validator is properly initialized
	assert.NotNil(t, utils.Validate)
}

func TestGenericValidator_Comprehensive(t *testing.T) {
	// Test struct for validation
	type TestValidationStruct struct {
		RequiredString string  `validate:"required"`
		Email          string  `validate:"email"`
		URL            string  `validate:"url"`
		MinLength      string  `validate:"min=3"`
		MaxLength      string  `validate:"max=10"`
		NumericRange   int     `validate:"min=1,max=100"`
		OptionalField  *string `validate:"omitempty,email"`
	}

	tests := []struct {
		name          string
		input         *TestValidationStruct
		expectedError bool
		description   string
	}{
		{
			name: "valid struct",
			input: &TestValidationStruct{
				RequiredString: "test",
				Email:          "test@example.com",
				URL:            "https://example.com",
				MinLength:      "test",
				MaxLength:      "short",
				NumericRange:   50,
				OptionalField:  nil,
			},
			expectedError: false,
			description:   "All fields are valid",
		},
		{
			name: "missing required field",
			input: &TestValidationStruct{
				RequiredString: "", // Missing required field
				Email:          "test@example.com",
				URL:            "https://example.com",
				MinLength:      "test",
				MaxLength:      "short",
				NumericRange:   50,
			},
			expectedError: true,
			description:   "Required field is empty",
		},
		{
			name: "invalid email",
			input: &TestValidationStruct{
				RequiredString: "test",
				Email:          "invalid-email", // Invalid email
				URL:            "https://example.com",
				MinLength:      "test",
				MaxLength:      "short",
				NumericRange:   50,
			},
			expectedError: true,
			description:   "Email field is invalid",
		},
		{
			name: "invalid URL",
			input: &TestValidationStruct{
				RequiredString: "test",
				Email:          "test@example.com",
				URL:            "not-a-url", // Invalid URL
				MinLength:      "test",
				MaxLength:      "short",
				NumericRange:   50,
			},
			expectedError: true,
			description:   "URL field is invalid",
		},
		{
			name: "string too short",
			input: &TestValidationStruct{
				RequiredString: "test",
				Email:          "test@example.com",
				URL:            "https://example.com",
				MinLength:      "ab", // Too short (min=3)
				MaxLength:      "short",
				NumericRange:   50,
			},
			expectedError: true,
			description:   "String is shorter than minimum length",
		},
		{
			name: "string too long",
			input: &TestValidationStruct{
				RequiredString: "test",
				Email:          "test@example.com",
				URL:            "https://example.com",
				MinLength:      "test",
				MaxLength:      "this string is way too long", // Too long (max=10)
				NumericRange:   50,
			},
			expectedError: true,
			description:   "String is longer than maximum length",
		},
		{
			name: "number too small",
			input: &TestValidationStruct{
				RequiredString: "test",
				Email:          "test@example.com",
				URL:            "https://example.com",
				MinLength:      "test",
				MaxLength:      "short",
				NumericRange:   0, // Too small (min=1)
			},
			expectedError: true,
			description:   "Number is below minimum range",
		},
		{
			name: "number too large",
			input: &TestValidationStruct{
				RequiredString: "test",
				Email:          "test@example.com",
				URL:            "https://example.com",
				MinLength:      "test",
				MaxLength:      "short",
				NumericRange:   150, // Too large (max=100)
			},
			expectedError: true,
			description:   "Number is above maximum range",
		},
		{
			name: "valid with optional field",
			input: &TestValidationStruct{
				RequiredString: "test",
				Email:          "test@example.com",
				URL:            "https://example.com",
				MinLength:      "test",
				MaxLength:      "short",
				NumericRange:   50,
				OptionalField:  stringPtr("valid@email.com"),
			},
			expectedError: false,
			description:   "Valid struct with valid optional field",
		},
		{
			name: "invalid optional field",
			input: &TestValidationStruct{
				RequiredString: "test",
				Email:          "test@example.com",
				URL:            "https://example.com",
				MinLength:      "test",
				MaxLength:      "short",
				NumericRange:   50,
				OptionalField:  stringPtr("invalid-email"), // Invalid optional email
			},
			expectedError: true,
			description:   "Optional field is present but invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := GenericValidator(tt.input)
			if tt.expectedError {
				assert.Error(t, err, tt.description)
			} else {
				assert.NoError(t, err, tt.description)
			}
		})
	}
}

func TestGenericValidator_EdgeCases(t *testing.T) {
	t.Run("nil struct pointer", func(t *testing.T) {
		var nilStruct *struct {
			Field string `validate:"required"`
		}
		err := GenericValidator(nilStruct)
		assert.Error(t, err)
	})

	t.Run("non-struct type", func(t *testing.T) {
		stringValue := "test"
		err := GenericValidator(&stringValue)
		assert.Error(t, err)
	})

	t.Run("struct with no validation tags", func(t *testing.T) {
		type NoValidationStruct struct {
			Field1 string
			Field2 int
		}
		input := &NoValidationStruct{
			Field1: "test",
			Field2: 42,
		}
		err := GenericValidator(input)
		assert.NoError(t, err)
	})

	t.Run("empty struct", func(t *testing.T) {
		type EmptyStruct struct{}
		input := &EmptyStruct{}
		err := GenericValidator(input)
		assert.NoError(t, err)
	})
}

func TestGenericUnmarshal_Comprehensive(t *testing.T) {
	// Test struct for unmarshaling
	type TestUnmarshalStruct struct {
		StringField string  `json:"string_field"`
		IntField    int     `json:"int_field"`
		BoolField   bool    `json:"bool_field"`
		FloatField  float64 `json:"float_field"`
	}

	tests := []struct {
		name          string
		input         string
		expectedError bool
		expectedData  *TestUnmarshalStruct
		description   string
	}{
		{
			name:          "valid complete JSON",
			input:         `{"string_field": "test", "int_field": 42, "bool_field": true, "float_field": 3.14}`,
			expectedError: false,
			expectedData: &TestUnmarshalStruct{
				StringField: "test",
				IntField:    42,
				BoolField:   true,
				FloatField:  3.14,
			},
			description: "All fields present and valid",
		},
		{
			name:          "partial JSON",
			input:         `{"string_field": "test", "int_field": 42}`,
			expectedError: false,
			expectedData: &TestUnmarshalStruct{
				StringField: "test",
				IntField:    42,
				BoolField:   false, // default value
				FloatField:  0,     // default value
			},
			description: "Only some fields present",
		},
		{
			name:          "empty JSON object",
			input:         `{}`,
			expectedError: false,
			expectedData: &TestUnmarshalStruct{
				StringField: "",    // default value
				IntField:    0,     // default value
				BoolField:   false, // default value
				FloatField:  0,     // default value
			},
			description: "Empty JSON object",
		},
		{
			name:          "malformed JSON - missing quote",
			input:         `{"string_field": test", "int_field": 42}`,
			expectedError: true,
			expectedData:  nil,
			description:   "JSON with syntax error",
		},
		{
			name:          "malformed JSON - trailing comma",
			input:         `{"string_field": "test", "int_field": 42,}`,
			expectedError: true,
			expectedData:  nil,
			description:   "JSON with trailing comma",
		},
		{
			name:          "wrong data type",
			input:         `{"string_field": 123, "int_field": "not_a_number"}`,
			expectedError: true,
			expectedData:  nil,
			description:   "JSON with wrong data types",
		},
		{
			name:          "unknown fields",
			input:         `{"string_field": "test", "unknown_field": "value"}`,
			expectedError: true,
			expectedData:  nil,
			description:   "JSON with unknown fields (DisallowUnknownFields is set)",
		},
		{
			name:          "null JSON",
			input:         `null`,
			expectedError: false,
			expectedData: &TestUnmarshalStruct{
				StringField: "",
				IntField:    0,
				BoolField:   false,
				FloatField:  0,
			},
			description: "Null JSON value",
		},
		{
			name:          "empty string",
			input:         "",
			expectedError: true,
			expectedData:  nil,
			description:   "Empty input string",
		},
		{
			name:          "whitespace only",
			input:         "   \n\t  ",
			expectedError: true,
			expectedData:  nil,
			description:   "Whitespace only input",
		},
		{
			name:          "array instead of object",
			input:         `["test", 42, true]`,
			expectedError: true,
			expectedData:  nil,
			description:   "Array instead of expected object",
		},
		{
			name:          "string instead of object",
			input:         `"just a string"`,
			expectedError: true,
			expectedData:  nil,
			description:   "String instead of expected object",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := GenericUnmarshal[TestUnmarshalStruct](tt.input)

			if tt.expectedError {
				assert.Error(t, err, tt.description)
				assert.Nil(t, result, tt.description)
			} else {
				assert.NoError(t, err, tt.description)
				assert.NotNil(t, result, tt.description)
				assert.Equal(t, tt.expectedData, result, tt.description)
			}
		})
	}
}

func TestGenericUnmarshal_NestedStructures(t *testing.T) {
	type NestedStruct struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}

	type ComplexStruct struct {
		Nested      NestedStruct   `json:"nested"`
		NestedSlice []NestedStruct `json:"nested_slice"`
		SimpleSlice []string       `json:"simple_slice"`
		MapField    map[string]int `json:"map_field"`
	}

	tests := []struct {
		name          string
		input         string
		expectedError bool
		checkResult   func(t *testing.T, result *ComplexStruct)
	}{
		{
			name: "valid nested structure",
			input: `{
				"nested": {"id": "test-id", "name": "test-name"},
				"nested_slice": [{"id": "1", "name": "first"}, {"id": "2", "name": "second"}],
				"simple_slice": ["item1", "item2", "item3"],
				"map_field": {"key1": 10, "key2": 20}
			}`,
			expectedError: false,
			checkResult: func(t *testing.T, result *ComplexStruct) {
				assert.Equal(t, "test-id", result.Nested.ID)
				assert.Equal(t, "test-name", result.Nested.Name)
				assert.Len(t, result.NestedSlice, 2)
				assert.Equal(t, "1", result.NestedSlice[0].ID)
				assert.Equal(t, "first", result.NestedSlice[0].Name)
				assert.Len(t, result.SimpleSlice, 3)
				assert.Equal(t, "item1", result.SimpleSlice[0])
				assert.Equal(t, 10, result.MapField["key1"])
				assert.Equal(t, 20, result.MapField["key2"])
			},
		},
		{
			name: "malformed nested structure",
			input: `{
				"nested": {"id": "test-id", "name": 123},
				"nested_slice": "not an array"
			}`,
			expectedError: true,
			checkResult:   nil,
		},
		{
			name: "partial nested structure",
			input: `{
				"nested": {"id": "test-id"}
			}`,
			expectedError: false,
			checkResult: func(t *testing.T, result *ComplexStruct) {
				assert.Equal(t, "test-id", result.Nested.ID)
				assert.Equal(t, "", result.Nested.Name) // default value
				assert.Nil(t, result.NestedSlice)       // nil slice
				assert.Nil(t, result.SimpleSlice)       // nil slice
				assert.Nil(t, result.MapField)          // nil map
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := GenericUnmarshal[ComplexStruct](tt.input)

			if tt.expectedError {
				assert.Error(t, err)
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				if tt.checkResult != nil {
					tt.checkResult(t, result)
				}
			}
		})
	}
}

func TestGenericUnmarshal_VariousTypes(t *testing.T) {
	t.Run("simple string struct", func(t *testing.T) {
		type StringStruct struct {
			Value string `json:"value"`
		}

		result, err := GenericUnmarshal[StringStruct](`{"value": "test"}`)
		assert.NoError(t, err)
		assert.Equal(t, "test", result.Value)
	})

	t.Run("simple int struct", func(t *testing.T) {
		type IntStruct struct {
			Value int `json:"value"`
		}

		result, err := GenericUnmarshal[IntStruct](`{"value": 42}`)
		assert.NoError(t, err)
		assert.Equal(t, 42, result.Value)
	})

	t.Run("struct with pointers", func(t *testing.T) {
		type PointerStruct struct {
			StringPtr *string `json:"string_ptr"`
			IntPtr    *int    `json:"int_ptr"`
		}

		result, err := GenericUnmarshal[PointerStruct](`{"string_ptr": "test", "int_ptr": 42}`)
		assert.NoError(t, err)
		assert.NotNil(t, result.StringPtr)
		assert.NotNil(t, result.IntPtr)
		assert.Equal(t, "test", *result.StringPtr)
		assert.Equal(t, 42, *result.IntPtr)
	})

	t.Run("struct with null pointer fields", func(t *testing.T) {
		type PointerStruct struct {
			StringPtr *string `json:"string_ptr"`
			IntPtr    *int    `json:"int_ptr"`
		}

		result, err := GenericUnmarshal[PointerStruct](`{"string_ptr": null, "int_ptr": null}`)
		assert.NoError(t, err)
		assert.Nil(t, result.StringPtr)
		assert.Nil(t, result.IntPtr)
	})
}

// Helper function to create string pointer
func stringPtr(s string) *string {
	return &s
}
