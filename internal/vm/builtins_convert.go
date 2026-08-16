package vm

import (
	"errors"
	"fmt"
	"math"
	"strconv"

	"noxy-vm/internal/value"
)

// conversionInputLimit bounds how much of a rejected value appears in an error
// message, measured in characters.
const conversionInputLimit = 64

// int64 bounds as exactly representable float64 values (-2^63 and 2^63).
const (
	minInt64AsFloat        = -9223372036854775808.0
	maxInt64ExclusiveFloat = 9223372036854775808.0
)

func truncateConversionInput(text string) string {
	characters := []rune(text)
	if len(characters) <= conversionInputLimit {
		return text
	}
	return string(characters[:conversionInputLimit]) + "..."
}

// conversionTypeName refines runtimeValueMode, which reports every heap value
// as "object", so a rejected string is named as a string.
func conversionTypeName(item value.Value) string {
	if item.Type == value.VAL_OBJ {
		if _, ok := item.Obj.(string); ok {
			return "string"
		}
	}
	return runtimeValueMode(item)
}

func describeConversionInput(item value.Value) string {
	switch item.Type {
	case value.VAL_OBJ, value.VAL_BYTES:
		if text, ok := item.Obj.(string); ok {
			return fmt.Sprintf("%s %q", conversionTypeName(item), truncateConversionInput(text))
		}
	}
	return fmt.Sprintf("%s %s", conversionTypeName(item), truncateConversionInput(item.String()))
}

func convertValueToInt(item value.Value) (int64, error) {
	switch item.Type {
	case value.VAL_INT:
		return item.AsInt, nil
	case value.VAL_FLOAT:
		if math.IsNaN(item.AsFloat) || math.IsInf(item.AsFloat, 0) {
			return 0, fmt.Errorf("cannot convert %s to int", describeConversionInput(item))
		}
		truncated := math.Trunc(item.AsFloat)
		if truncated < minInt64AsFloat || truncated >= maxInt64ExclusiveFloat {
			return 0, fmt.Errorf("cannot convert %s to int: out of range", describeConversionInput(item))
		}
		return int64(truncated), nil
	case value.VAL_OBJ:
		if text, ok := item.Obj.(string); ok {
			parsed, parseErr := strconv.ParseInt(text, 10, 64)
			if parseErr != nil {
				if errors.Is(parseErr, strconv.ErrRange) {
					return 0, fmt.Errorf("cannot convert %s to int: out of range", describeConversionInput(item))
				}
				return 0, fmt.Errorf("cannot convert %s to int", describeConversionInput(item))
			}
			return parsed, nil
		}
	}
	return 0, fmt.Errorf("cannot convert %s to int", describeConversionInput(item))
}

func convertValueToFloat(item value.Value) (float64, error) {
	switch item.Type {
	case value.VAL_FLOAT:
		return item.AsFloat, nil
	case value.VAL_INT:
		return float64(item.AsInt), nil
	case value.VAL_OBJ:
		if text, ok := item.Obj.(string); ok {
			parsed, parseErr := strconv.ParseFloat(text, 64)
			if parseErr != nil {
				if errors.Is(parseErr, strconv.ErrRange) {
					return 0, fmt.Errorf("cannot convert %s to float: out of range", describeConversionInput(item))
				}
				return 0, fmt.Errorf("cannot convert %s to float", describeConversionInput(item))
			}
			return parsed, nil
		}
	}
	return 0, fmt.Errorf("cannot convert %s to float", describeConversionInput(item))
}

func conversionResult(ok bool, converted value.Value, reason string) value.Value {
	return value.NewMapWithData(map[string]value.Value{
		"ok":    value.NewBool(ok),
		"value": converted,
		"error": value.NewString(reason),
	})
}

func (vm *VM) defineConvertBuiltins() {
	vm.DefineNative("convert_to_int_result", func(args []value.Value) value.Value {
		if len(args) != 1 {
			return conversionResult(false, value.NewInt(0),
				fmt.Sprintf("to_int_result expects exactly 1 argument, got %d", len(args)))
		}
		converted, convertErr := convertValueToInt(args[0])
		if convertErr != nil {
			return conversionResult(false, value.NewInt(0), convertErr.Error())
		}
		return conversionResult(true, value.NewInt(converted), "")
	})

	vm.DefineNative("convert_to_float_result", func(args []value.Value) value.Value {
		if len(args) != 1 {
			return conversionResult(false, value.NewFloat(0),
				fmt.Sprintf("to_float_result expects exactly 1 argument, got %d", len(args)))
		}
		converted, convertErr := convertValueToFloat(args[0])
		if convertErr != nil {
			return conversionResult(false, value.NewFloat(0), convertErr.Error())
		}
		return conversionResult(true, value.NewFloat(converted), "")
	})
}
