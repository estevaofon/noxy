package vm

import (
	"testing"
	"time"

	"github.com/estevaofon/noxy/internal/value"
)

func testDateTimeDefinition() value.Value {
	return value.NewStruct("DateTime", []string{
		"year", "month", "day", "hour", "minute", "second",
		"weekday", "yearday", "timestamp",
	})
}

func requireBuiltinInstance(t *testing.T, got value.Value, definition value.Value) *value.ObjInstance {
	t.Helper()
	if got.Type != value.VAL_OBJ {
		t.Fatalf("type = %v, want object", got.Type)
	}
	instance, ok := got.Obj.(*value.ObjInstance)
	if !ok {
		t.Fatalf("payload = %#v, want *value.ObjInstance", got.Obj)
	}
	if instance.Struct != definition.Obj.(*value.ObjStruct) {
		t.Fatal("instance does not use the supplied struct definition")
	}
	return instance
}

func assertDateTimeFields(t *testing.T, instance *value.ObjInstance, expected time.Time) {
	t.Helper()
	fields := map[string]int64{
		"year":      int64(expected.Year()),
		"month":     int64(expected.Month()),
		"day":       int64(expected.Day()),
		"hour":      int64(expected.Hour()),
		"minute":    int64(expected.Minute()),
		"second":    int64(expected.Second()),
		"weekday":   int64(expected.Weekday()),
		"yearday":   int64(expected.YearDay()),
		"timestamp": expected.Unix(),
	}
	for name, want := range fields {
		t.Run(name, func(t *testing.T) {
			got, ok := instance.Get(name)
			if !ok {
				t.Fatalf("missing field %q", name)
			}
			assertBuiltinValue(t, got, value.NewInt(want))
		})
	}
}

func TestTimeDateTimeConstructionAndConversion(t *testing.T) {
	machine := New()
	definition := testDateTimeDefinition()
	expected := time.Date(2024, time.February, 29, 14, 5, 6, 0, time.Local)
	made := callBuiltin(t, machine, "time_make_datetime",
		definition,
		value.NewInt(2024), value.NewInt(2), value.NewInt(29),
		value.NewInt(14), value.NewInt(5), value.NewInt(6),
	)
	instance := requireBuiltinInstance(t, made, definition)
	assertDateTimeFields(t, instance, expected)
	assertBuiltinValue(t, callBuiltin(t, machine, "time_to_timestamp", made), value.NewInt(expected.Unix()))

	fromTimestamp := callBuiltin(t, machine, "time_from_timestamp", value.NewInt(expected.Unix()), definition)
	assertDateTimeFields(t, requireBuiltinInstance(t, fromTimestamp, definition), time.Unix(expected.Unix(), 0))

	assertBuiltinValue(t, callBuiltin(t, machine, "time_make_datetime", definition), value.NewNull())
	assertBuiltinValue(t, callBuiltin(t, machine, "time_to_timestamp"), value.NewInt(0))
	assertBuiltinValue(t, callBuiltin(t, machine, "time_from_timestamp", value.NewInt(expected.Unix())), value.NewNull())
}

func TestTimeArithmeticAndCalendarBuiltins(t *testing.T) {
	machine := New()
	tests := []struct {
		name    string
		builtin string
		args    []value.Value
		want    value.Value
	}{
		{name: "diff positive signed", builtin: "time_diff", args: []value.Value{value.NewInt(200), value.NewInt(125)}, want: value.NewInt(75)},
		{name: "diff negative signed", builtin: "time_diff", args: []value.Value{value.NewInt(125), value.NewInt(200)}, want: value.NewInt(-75)},
		{name: "diff short", builtin: "time_diff", args: []value.Value{value.NewInt(1)}, want: value.NewInt(0)},
		{name: "add days", builtin: "time_add_days", args: []value.Value{value.NewInt(100), value.NewInt(2)}, want: value.NewInt(172900)},
		{name: "add negative days", builtin: "time_add_days", args: []value.Value{value.NewInt(100), value.NewInt(-1)}, want: value.NewInt(-86300)},
		{name: "add days short", builtin: "time_add_days", want: value.NewInt(0)},
		{name: "add seconds", builtin: "time_add_seconds", args: []value.Value{value.NewInt(100), value.NewInt(23)}, want: value.NewInt(123)},
		{name: "add seconds short", builtin: "time_add_seconds", args: []value.Value{value.NewInt(100)}, want: value.NewInt(0)},
		{name: "before true", builtin: "time_before", args: []value.Value{value.NewInt(1), value.NewInt(2)}, want: value.NewBool(true)},
		{name: "before equal", builtin: "time_before", args: []value.Value{value.NewInt(2), value.NewInt(2)}, want: value.NewBool(false)},
		{name: "before short", builtin: "time_before", want: value.NewBool(false)},
		{name: "after true", builtin: "time_after", args: []value.Value{value.NewInt(2), value.NewInt(1)}, want: value.NewBool(true)},
		{name: "after equal", builtin: "time_after", args: []value.Value{value.NewInt(2), value.NewInt(2)}, want: value.NewBool(false)},
		{name: "after short", builtin: "time_after", want: value.NewBool(false)},
		{name: "leap divisible by 400", builtin: "time_is_leap_year", args: []value.Value{value.NewInt(2000)}, want: value.NewBool(true)},
		{name: "century not leap", builtin: "time_is_leap_year", args: []value.Value{value.NewInt(1900)}, want: value.NewBool(false)},
		{name: "leap short", builtin: "time_is_leap_year", want: value.NewBool(false)},
		{name: "days in leap february", builtin: "time_days_in_month", args: []value.Value{value.NewInt(2024), value.NewInt(2)}, want: value.NewInt(29)},
		{name: "days in april", builtin: "time_days_in_month", args: []value.Value{value.NewInt(2023), value.NewInt(4)}, want: value.NewInt(30)},
		{name: "days in month short", builtin: "time_days_in_month", args: []value.Value{value.NewInt(2024)}, want: value.NewInt(0)},
		{name: "weekday sunday", builtin: "time_weekday_name", args: []value.Value{value.NewInt(0)}, want: value.NewString("Domingo")},
		{name: "weekday saturday", builtin: "time_weekday_name", args: []value.Value{value.NewInt(6)}, want: value.NewString("Sábado")},
		{name: "weekday short", builtin: "time_weekday_name", want: value.NewString("")},
		{name: "month march", builtin: "time_month_name", args: []value.Value{value.NewInt(3)}, want: value.NewString("Março")},
		{name: "month december", builtin: "time_month_name", args: []value.Value{value.NewInt(12)}, want: value.NewString("Dezembro")},
		{name: "month short", builtin: "time_month_name", want: value.NewString("")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertBuiltinValue(t, callBuiltin(t, machine, tt.builtin, tt.args...), tt.want)
		})
	}
}

func TestTimeDurationBuiltin(t *testing.T) {
	machine := New()
	definition := value.NewStruct("Duration", []string{"days", "hours", "minutes", "seconds", "total_seconds"})
	got := callBuiltin(t, machine, "time_diff_duration", value.NewInt(100), value.NewInt(90161), definition)
	instance := requireBuiltinInstance(t, got, definition)
	expected := map[string]int64{
		"days": 1, "hours": 1, "minutes": 1, "seconds": 1, "total_seconds": -90061,
	}
	for name, want := range expected {
		assertBuiltinValue(t, instance.Field(name), value.NewInt(want))
	}
	assertBuiltinValue(t, callBuiltin(t, machine, "time_diff_duration", value.NewInt(1), value.NewInt(2)), value.NewNull())
}

func TestTimeParsingAndFormattingBuiltins(t *testing.T) {
	machine := New()
	definition := testDateTimeDefinition()
	expected := time.Date(2024, time.February, 29, 14, 5, 6, 0, time.Local)
	parsed := callBuiltin(t, machine, "time_parse", value.NewString("2024-02-29 14:05:06"), definition)
	instance := requireBuiltinInstance(t, parsed, definition)
	assertDateTimeFields(t, instance, expected)

	formatTests := []struct {
		name    string
		builtin string
		args    []value.Value
		want    string
	}{
		{name: "datetime", builtin: "time_format", args: []value.Value{parsed}, want: "2024-02-29 14:05:06"},
		{name: "date", builtin: "time_format_date", args: []value.Value{parsed}, want: "2024-02-29"},
		{name: "time", builtin: "time_format_time", args: []value.Value{parsed}, want: "14:05:06"},
		{name: "custom", builtin: "time_format_custom", args: []value.Value{parsed, value.NewString("%d/%m/%Y %H:%M:%S")}, want: "29/02/2024 14:05:06"},
		{name: "datetime short", builtin: "time_format", want: ""},
		{name: "date short", builtin: "time_format_date", want: ""},
		{name: "time short", builtin: "time_format_time", want: ""},
		{name: "custom short", builtin: "time_format_custom", args: []value.Value{parsed}, want: ""},
	}
	for _, tt := range formatTests {
		t.Run(tt.name, func(t *testing.T) {
			assertBuiltinValue(t, callBuiltin(t, machine, tt.builtin, tt.args...), value.NewString(tt.want))
		})
	}

	parsedDate := callBuiltin(t, machine, "time_parse_date", value.NewString("2024-02-29"), definition)
	assertDateTimeFields(t, requireBuiltinInstance(t, parsedDate, definition), time.Date(2024, time.February, 29, 0, 0, 0, 0, time.Local))
	assertBuiltinValue(t, callBuiltin(t, machine, "time_parse", value.NewString("not-a-date"), definition), value.NewNull())
	assertBuiltinValue(t, callBuiltin(t, machine, "time_parse", value.NewString("2024-02-29 14:05:06")), value.NewNull())
	assertBuiltinValue(t, callBuiltin(t, machine, "time_parse_date", value.NewString("2024-02-30"), definition), value.NewNull())
	assertBuiltinValue(t, callBuiltin(t, machine, "time_parse_date", value.NewString("2024-02-29")), value.NewNull())
}

func TestTimeNowBuiltinsReturnStableShapes(t *testing.T) {
	machine := New()
	if got := callBuiltin(t, machine, "time_now"); got.Type != value.VAL_INT {
		t.Fatalf("time_now type = %v, want int", got.Type)
	}
	if got := callBuiltin(t, machine, "time_now_ms"); got.Type != value.VAL_INT {
		t.Fatalf("time_now_ms type = %v, want int", got.Type)
	}

	definition := testDateTimeDefinition()
	instance := requireBuiltinInstance(t, callBuiltin(t, machine, "time_now_datetime", definition), definition)
	for _, field := range []string{"year", "month", "day", "hour", "minute", "second", "weekday", "yearday", "timestamp"} {
		if got, ok := instance.Get(field); !ok || got.Type != value.VAL_INT {
			t.Errorf("field %q = %#v, want int", field, got)
		}
	}
}
