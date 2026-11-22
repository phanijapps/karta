package skillhub

import (
	"encoding/json"
	"io"
)

// jsonDecodeLimited decodes one JSON document with the envelope bound.
func jsonDecodeLimited(r io.Reader, dst any) error {
	return json.NewDecoder(io.LimitReader(r, 1<<20)).Decode(dst)
}
