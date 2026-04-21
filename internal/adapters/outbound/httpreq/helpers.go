package httpreq

import (
	"bytes"
	"encoding/json"
	"time"

	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
)

// decodeStrict JSON-decodes payload into v with DisallowUnknownFields.
func decodeStrict(payload valueobjects.Payload, v interface{}) error {
	dec := json.NewDecoder(bytes.NewReader(payload.Raw()))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// elapsedMs returns milliseconds elapsed since t0 using the adapter's clock.
func (a *Adapter) elapsedMs(t0 time.Time) int64 {
	return a.clk.Now().Sub(t0).Milliseconds()
}
