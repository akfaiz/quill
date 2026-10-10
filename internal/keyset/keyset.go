// Package keyset encodes the opaque token that carries a keyset-pagination
// position between requests. Tokens come from clients, so Decode treats its input as
// untrusted: it never panics, and only ever yields a string, bool, int64 or float64
// that the caller binds as a query argument.
package keyset

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/akfaiz/quill/internal/errs"
)

// Encode turns the last row's key value into a URL-safe token. []byte becomes a
// string and time.Time an RFC 3339 string.
func Encode(v any) (string, error) {
	switch x := v.(type) {
	case []byte:
		v = string(x)
	case time.Time:
		v = x.Format(time.RFC3339Nano)
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("%w: cursor value: %v", errs.ErrInvalidArgs, err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func Decode(s string) (any, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("%w: malformed cursor", errs.ErrInvalidArgs)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("%w: malformed cursor", errs.ErrInvalidArgs)
	}
	switch x := v.(type) {
	case json.Number:
		if n, err := x.Int64(); err == nil {
			return n, nil
		}
		f, err := x.Float64()
		if err != nil {
			return nil, fmt.Errorf("%w: cursor number out of range", errs.ErrInvalidArgs)
		}
		return f, nil
	case string, bool:
		return x, nil
	}
	return nil, fmt.Errorf("%w: cursor holds an unsupported value", errs.ErrInvalidArgs)
}
