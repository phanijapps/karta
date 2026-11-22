package chitta

import (
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"strings"
	"sync"
	"time"
)

// newID mints a prefixed, sortable id: prefix + 48-bit millisecond
// timestamp + 80 random bits, Crockford-style base32. The service layer
// mints ids — never the driver, never the agent.
var idMu sync.Mutex
var lastTime int64

func newID(prefix string) string {
	idMu.Lock()
	now := time.Now().UTC().UnixMilli()
	if now <= lastTime {
		now = lastTime + 1 // monotonic under bursts
	}
	lastTime = now
	idMu.Unlock()

	var rnd [10]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		panic("chitta: crypto/rand unavailable: " + err.Error())
	}
	enc := base32.NewEncoding("0123456789ABCDEFGHJKMNPQRSTVWXYZ").WithPadding(base32.NoPadding)
	timePart := enc.EncodeToString([]byte{
		byte(now >> 32), byte(now >> 24), byte(now >> 16), byte(now >> 8), byte(now),
	})
	randPart := strings.ToLower(enc.EncodeToString(rnd[:]))
	return fmt.Sprintf("%s_%s%s", prefix, timePart, randPart[:16])
}
