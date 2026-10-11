package rowmap

import (
	"database/sql/driver"
	"fmt"
	"reflect"
	"strconv"
	"time"
)

// fieldScanner is a reusable sql.Scanner that stores one column into dst and leaves the
// zero value for NULL. It does what scanning through a **T does, without allocating a
// temporary pointer for every field of every row. Conversions follow database/sql's.
type fieldScanner struct{ dst reflect.Value }

// fastKind reports plain value types a fieldScanner can store.
func fastKind(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.String, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	}
	return t == timeType
}

func (s *fieldScanner) Scan(src any) error {
	dst := s.dst
	if src == nil {
		dst.SetZero()
		return nil
	}
	switch dst.Kind() {
	case reflect.String:
		switch v := src.(type) {
		case string:
			dst.SetString(v)
		case []byte:
			dst.SetString(string(v))
		default:
			dst.SetString(asString(src))
		}
	case reflect.Bool:
		b, err := driver.Bool.ConvertValue(src)
		if err != nil {
			return fmt.Errorf("converting driver.Value type %T (%q) to a bool: %w", src, asString(src), err)
		}
		dst.SetBool(b.(bool))
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if n, ok := src.(int64); ok {
			if dst.OverflowInt(n) {
				return convErr(src, dst, "value out of range")
			}
			dst.SetInt(n)
			return nil
		}
		str := asString(src)
		n, err := strconv.ParseInt(str, 10, dst.Type().Bits())
		if err != nil {
			return convErr(src, dst, err)
		}
		dst.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if n, ok := src.(int64); ok && n >= 0 && !dst.OverflowUint(uint64(n)) {
			dst.SetUint(uint64(n))
			return nil
		}
		n, err := strconv.ParseUint(asString(src), 10, dst.Type().Bits())
		if err != nil {
			return convErr(src, dst, err)
		}
		dst.SetUint(n)
	case reflect.Float32, reflect.Float64:
		if f, ok := src.(float64); ok && dst.Kind() == reflect.Float64 {
			dst.SetFloat(f)
			return nil
		}
		f, err := strconv.ParseFloat(asString(src), dst.Type().Bits())
		if err != nil {
			return convErr(src, dst, err)
		}
		dst.SetFloat(f)
	default: // time.Time
		t, ok := src.(time.Time)
		if !ok {
			return fmt.Errorf("unsupported Scan, storing driver.Value type %T into type *%s", src, dst.Type())
		}
		dst.Set(reflect.ValueOf(t))
	}
	return nil
}

func convErr(src any, dst reflect.Value, cause any) error {
	return fmt.Errorf("converting driver.Value type %T (%q) to a %s: %v", src, asString(src), dst.Kind(), cause)
}

// asString renders a driver value the way database/sql does before parsing it.
func asString(src any) string {
	switch v := src.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	case time.Time:
		return v.Format(time.RFC3339Nano)
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		return strconv.FormatFloat(v, 'g', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	}
	return fmt.Sprintf("%v", src)
}
