package vm

import (
	"fmt"
	"strings"
	"time"

	"noxy-vm/internal/value"
)

func (vm *VM) defineTimeBuiltins() {
	vm.DefineNative("time_now_ms", func(args []value.Value) value.Value {
		return value.NewInt(time.Now().UnixMilli())
	})
	vm.DefineNative("time_now", func(args []value.Value) value.Value {
		return value.NewInt(time.Now().Unix())
	})

	vm.DefineNative("time_sleep", func(args []value.Value) value.Value {
		if len(args) != 1 {
			return value.NewNull()
		}
		ms := args[0].Int()
		time.Sleep(time.Duration(ms) * time.Millisecond)
		return value.NewNull()
	})
	vm.DefineNative("time_now_datetime", func(args []value.Value) value.Value {
		// args[0] is DateTime struct def
		if len(args) < 1 {
			return value.NewNull()
		}
		structDef, ok := args[0].Obj.(*value.ObjStruct)
		if !ok {
			return value.NewNull()
		}

		t := time.Now()
		inst := value.NewInstance(structDef).Obj.(*value.ObjInstance)
		inst.Fields["year"] = value.NewInt(int64(t.Year()))
		inst.Fields["month"] = value.NewInt(int64(t.Month()))
		inst.Fields["day"] = value.NewInt(int64(t.Day()))
		inst.Fields["hour"] = value.NewInt(int64(t.Hour()))
		inst.Fields["minute"] = value.NewInt(int64(t.Minute()))
		inst.Fields["second"] = value.NewInt(int64(t.Second()))
		inst.Fields["weekday"] = value.NewInt(int64(t.Weekday()))
		inst.Fields["yearday"] = value.NewInt(int64(t.YearDay()))
		inst.Fields["timestamp"] = value.NewInt(t.Unix())

		return value.Value{Type: value.VAL_OBJ, Obj: inst}
	})
	vm.DefineNative("time_format", func(args []value.Value) value.Value {
		if len(args) < 1 {
			return value.NewString("")
		}
		inst, ok := args[0].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewString("")
		}

		// Reconstruct time.Time from fields
		// Minimal fields: year, month, day, hour, minute, second
		y := int(inst.Fields["year"].Int())
		m := time.Month(inst.Fields["month"].Int())
		d := int(inst.Fields["day"].Int())
		h := int(inst.Fields["hour"].Int())
		min := int(inst.Fields["minute"].Int())
		s := int(inst.Fields["second"].Int())

		t := time.Date(y, m, d, h, min, s, 0, time.Local)
		return value.NewString(t.Format("2006-01-02 15:04:05"))
	})
	vm.DefineNative("time_format_date", func(args []value.Value) value.Value {
		if len(args) < 1 {
			return value.NewString("")
		}
		inst, ok := args[0].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewString("")
		}
		y := int(inst.Fields["year"].Int())
		m := time.Month(inst.Fields["month"].Int())
		d := int(inst.Fields["day"].Int())
		t := time.Date(y, m, d, 0, 0, 0, 0, time.Local)
		return value.NewString(t.Format("2006-01-02"))
	})
	vm.DefineNative("time_format_time", func(args []value.Value) value.Value {
		if len(args) < 1 {
			return value.NewString("")
		}
		inst, ok := args[0].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewString("")
		}
		h := int(inst.Fields["hour"].Int())
		min := int(inst.Fields["minute"].Int())
		s := int(inst.Fields["second"].Int())
		t := time.Date(0, 1, 1, h, min, s, 0, time.Local)
		return value.NewString(t.Format("15:04:05"))
	})
	vm.DefineNative("time_make_datetime", func(args []value.Value) value.Value {
		// args: structDef, y, m, d, h, min, s
		if len(args) < 7 {
			return value.NewNull()
		}
		structDef, ok := args[0].Obj.(*value.ObjStruct)
		if !ok {
			return value.NewNull()
		}

		y := int(args[1].Int())
		m := time.Month(args[2].Int())
		d := int(args[3].Int())
		h := int(args[4].Int())
		min := int(args[5].Int())
		s := int(args[6].Int())

		t := time.Date(y, m, d, h, min, s, 0, time.Local)

		inst := value.NewInstance(structDef).Obj.(*value.ObjInstance)
		inst.Fields["year"] = value.NewInt(int64(t.Year()))
		inst.Fields["month"] = value.NewInt(int64(t.Month()))
		inst.Fields["day"] = value.NewInt(int64(t.Day()))
		inst.Fields["hour"] = value.NewInt(int64(t.Hour()))
		inst.Fields["minute"] = value.NewInt(int64(t.Minute()))
		inst.Fields["second"] = value.NewInt(int64(t.Second()))
		inst.Fields["weekday"] = value.NewInt(int64(t.Weekday()))
		inst.Fields["yearday"] = value.NewInt(int64(t.YearDay()))
		inst.Fields["timestamp"] = value.NewInt(t.Unix())

		return value.Value{Type: value.VAL_OBJ, Obj: inst}
	})
	vm.DefineNative("time_to_timestamp", func(args []value.Value) value.Value {
		if len(args) < 1 {
			return value.NewInt(0)
		}
		inst, ok := args[0].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewInt(0)
		}

		val, ok := inst.Fields["timestamp"]
		if ok {
			return val
		}
		return value.NewInt(0)
	})
	vm.DefineNative("time_from_timestamp", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewNull()
		}
		ts := args[0].Int()
		structDef, ok := args[1].Obj.(*value.ObjStruct)
		if !ok {
			return value.NewNull()
		}

		t := time.Unix(ts, 0)
		inst := value.NewInstance(structDef).Obj.(*value.ObjInstance)
		inst.Fields["year"] = value.NewInt(int64(t.Year()))
		inst.Fields["month"] = value.NewInt(int64(t.Month()))
		inst.Fields["day"] = value.NewInt(int64(t.Day()))
		inst.Fields["hour"] = value.NewInt(int64(t.Hour()))
		inst.Fields["minute"] = value.NewInt(int64(t.Minute()))
		inst.Fields["second"] = value.NewInt(int64(t.Second()))
		inst.Fields["weekday"] = value.NewInt(int64(t.Weekday()))
		inst.Fields["yearday"] = value.NewInt(int64(t.YearDay()))
		inst.Fields["timestamp"] = value.NewInt(t.Unix())

		return value.Value{Type: value.VAL_OBJ, Obj: inst}
	})
	vm.DefineNative("time_diff", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewInt(0)
		}
		ts1 := args[0].Int()
		ts2 := args[1].Int()
		return value.NewInt(ts1 - ts2)
	})
	vm.DefineNative("time_add_days", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewInt(0)
		}
		ts := args[0].Int()
		days := args[1].Int()
		return value.NewInt(ts + (days * 86400))
	})
	vm.DefineNative("time_before", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewBool(false)
		}
		return value.NewBool(args[0].Int() < args[1].Int())
	})
	vm.DefineNative("time_after", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewBool(false)
		}
		return value.NewBool(args[0].Int() > args[1].Int())
	})
	vm.DefineNative("time_is_leap_year", func(args []value.Value) value.Value {
		if len(args) < 1 {
			return value.NewBool(false)
		}
		year := args[0].Int()
		return value.NewBool(year%4 == 0 && (year%100 != 0 || year%400 == 0))
	})
	vm.DefineNative("time_days_in_month", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewInt(0)
		}
		year := int(args[0].Int())
		month := time.Month(args[1].Int())
		// Trick: go to next month day 0
		t := time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC)
		return value.NewInt(int64(t.Day()))
	})
	vm.DefineNative("time_weekday_name", func(args []value.Value) value.Value {
		if len(args) < 1 {
			return value.NewString("")
		}
		wd := time.Weekday(args[0].Int())

		names := []string{
			"Domingo", "Segunda-feira", "Terça-feira", "Quarta-feira",
			"Quinta-feira", "Sexta-feira", "Sábado",
		}
		if int(wd) >= 0 && int(wd) < len(names) {
			return value.NewString(names[wd])
		}
		return value.NewString(wd.String())
	})
	vm.DefineNative("time_month_name", func(args []value.Value) value.Value {
		if len(args) < 1 {
			return value.NewString("")
		}
		m := time.Month(args[0].Int())
		names := map[time.Month]string{
			time.January: "Janeiro", time.February: "Fevereiro", time.March: "Março",
			time.April: "Abril", time.May: "Maio", time.June: "Junho",
			time.July: "Julho", time.August: "Agosto", time.September: "Setembro",
			time.October: "Outubro", time.November: "Novembro", time.December: "Dezembro",
		}
		if name, ok := names[m]; ok {
			return value.NewString(name)
		}
		return value.NewString(m.String())
	})
	vm.DefineNative("time_format_custom", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewString("")
		}
		inst, ok := args[0].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewString("")
		}
		fmtStr := args[1].Obj.(string)

		y := int(inst.Fields["year"].Int())
		m := time.Month(inst.Fields["month"].Int())
		d := int(inst.Fields["day"].Int())
		h := int(inst.Fields["hour"].Int())
		min := int(inst.Fields["minute"].Int())
		s := int(inst.Fields["second"].Int())
		// t := time.Date(y, m, d, h, min, s, 0, time.Local) // Unused in this simple implementation

		// Simplified replacement for strftime
		// Noxy: %Y=ano, %m=mês, %d=dia, %H=hora, %M=min, %S=seg
		res := fmtStr
		res = strings.ReplaceAll(res, "%Y", fmt.Sprintf("%04d", y))
		res = strings.ReplaceAll(res, "%m", fmt.Sprintf("%02d", m))
		res = strings.ReplaceAll(res, "%d", fmt.Sprintf("%02d", d))
		res = strings.ReplaceAll(res, "%H", fmt.Sprintf("%02d", h))
		res = strings.ReplaceAll(res, "%M", fmt.Sprintf("%02d", min))
		res = strings.ReplaceAll(res, "%S", fmt.Sprintf("%02d", s))

		return value.NewString(res)
	})
	vm.DefineNative("time_parse", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewNull()
		}
		str := args[0].Obj.(string)
		structDef, ok := args[1].Obj.(*value.ObjStruct)
		if !ok {
			return value.NewNull()
		}

		t, err := time.ParseInLocation("2006-01-02 15:04:05", str, time.Local)
		if err != nil {
			return value.NewNull()
		}

		inst := value.NewInstance(structDef).Obj.(*value.ObjInstance)
		inst.Fields["year"] = value.NewInt(int64(t.Year()))
		inst.Fields["month"] = value.NewInt(int64(t.Month()))
		inst.Fields["day"] = value.NewInt(int64(t.Day()))
		inst.Fields["hour"] = value.NewInt(int64(t.Hour()))
		inst.Fields["minute"] = value.NewInt(int64(t.Minute()))
		inst.Fields["second"] = value.NewInt(int64(t.Second()))
		inst.Fields["weekday"] = value.NewInt(int64(t.Weekday()))
		inst.Fields["yearday"] = value.NewInt(int64(t.YearDay()))
		inst.Fields["timestamp"] = value.NewInt(t.Unix())

		return value.Value{Type: value.VAL_OBJ, Obj: inst}
	})
	vm.DefineNative("time_parse_date", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewNull()
		}
		str := args[0].Obj.(string)
		structDef, ok := args[1].Obj.(*value.ObjStruct)
		if !ok {
			return value.NewNull()
		}

		t, err := time.ParseInLocation("2006-01-02", str, time.Local)
		if err != nil {
			return value.NewNull()
		}

		inst := value.NewInstance(structDef).Obj.(*value.ObjInstance)
		inst.Fields["year"] = value.NewInt(int64(t.Year()))
		inst.Fields["month"] = value.NewInt(int64(t.Month()))
		inst.Fields["day"] = value.NewInt(int64(t.Day()))
		inst.Fields["hour"] = value.NewInt(int64(t.Hour()))
		inst.Fields["minute"] = value.NewInt(int64(t.Minute()))
		inst.Fields["second"] = value.NewInt(int64(t.Second()))
		inst.Fields["weekday"] = value.NewInt(int64(t.Weekday()))
		inst.Fields["yearday"] = value.NewInt(int64(t.YearDay()))
		inst.Fields["timestamp"] = value.NewInt(t.Unix())

		return value.Value{Type: value.VAL_OBJ, Obj: inst}
	})
	vm.DefineNative("time_add_seconds", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewInt(0)
		}
		ts := args[0].Int()
		secs := args[1].Int()
		return value.NewInt(ts + secs)
	})
	vm.DefineNative("time_diff_duration", func(args []value.Value) value.Value {
		if len(args) < 3 {
			return value.NewNull()
		}
		ts1 := args[0].Int()
		ts2 := args[1].Int()
		structDef, ok := args[2].Obj.(*value.ObjStruct)
		if !ok {
			return value.NewNull()
		}

		diff := ts1 - ts2
		if diff < 0 {
			diff = -diff
		}

		totalSecs := ts1 - ts2
		absSecs := totalSecs
		if absSecs < 0 {
			absSecs = -absSecs
		}

		days := absSecs / 86400
		rem := absSecs % 86400
		hours := rem / 3600
		rem = rem % 3600
		mins := rem / 60
		secs := rem % 60

		inst := value.NewInstance(structDef).Obj.(*value.ObjInstance)
		inst.Fields["days"] = value.NewInt(days)
		inst.Fields["hours"] = value.NewInt(hours)
		inst.Fields["minutes"] = value.NewInt(mins)
		inst.Fields["seconds"] = value.NewInt(secs)
		inst.Fields["total_seconds"] = value.NewInt(totalSecs)

		return value.Value{Type: value.VAL_OBJ, Obj: inst}
	})
}
