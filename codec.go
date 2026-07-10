package d1

import (
	"database/sql/driver"
	"encoding/base64"
	"fmt"
	"math"
	"strings"
	"time"
)

const bytesPrefix = "d1b64:"

func EncodeParam(v any) (any, error) {
	switch value := v.(type) {
	case nil:
		return nil, nil
	case time.Time:
		return value.UTC().Format(time.RFC3339Nano), nil
	case bool:
		if value {
			return int64(1), nil
		}
		return int64(0), nil
	case []byte:
		return bytesPrefix + base64.StdEncoding.EncodeToString(value), nil
	case driver.Value:
		return value, nil
	default:
		return value, nil
	}
}

func EncodeParams(params []any) ([]any, error) {
	out := make([]any, len(params))
	for i, param := range params {
		encoded, err := EncodeParam(param)
		if err != nil {
			return nil, err
		}
		out[i] = encoded
	}
	return out, nil
}

func DecodeColumnValue(column string, value any) (driver.Value, error) {
	if value == nil {
		return nil, nil
	}
	lowerColumn := strings.ToLower(column)

	switch v := value.(type) {
	case bool:
		return v, nil
	case float64:
		if isBoolColumn(lowerColumn) && (v == 0 || v == 1) {
			return v == 1, nil
		}
		if math.Trunc(v) == v {
			return int64(v), nil
		}
		return v, nil
	case int64:
		if isBoolColumn(lowerColumn) && (v == 0 || v == 1) {
			return v == 1, nil
		}
		return v, nil
	case int:
		iv := int64(v)
		if isBoolColumn(lowerColumn) && (iv == 0 || iv == 1) {
			return iv == 1, nil
		}
		return iv, nil
	case string:
		if strings.HasPrefix(v, bytesPrefix) {
			decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(v, bytesPrefix))
			if err != nil {
				return nil, err
			}
			return decoded, nil
		}
		if isTimeColumn(lowerColumn) {
			if parsed, err := time.Parse(time.RFC3339Nano, v); err == nil {
				return parsed, nil
			}
		}
		return v, nil
	case []byte:
		return v, nil
	default:
		return nil, fmt.Errorf("unsupported D1 result value type %T", value)
	}
}

func isTimeColumn(column string) bool {
	return strings.Contains(column, "time") ||
		strings.HasSuffix(column, "_at") ||
		strings.HasSuffix(column, "date")
}

func isBoolColumn(column string) bool {
	return column == "flag" ||
		column == "active" ||
		strings.Contains(column, "bool") ||
		strings.HasPrefix(column, "is_") ||
		strings.HasPrefix(column, "has_") ||
		strings.HasPrefix(column, "can_")
}
