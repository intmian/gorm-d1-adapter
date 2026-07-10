package d1

import (
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"time"
)

var traceState = struct {
	sync.RWMutex
	enabled bool
	out     io.Writer
	redact  []string
}{out: io.Discard}

func Trace(pattern string, args ...any) {
	traceState.RLock()
	enabled := traceState.enabled
	out := traceState.out
	redact := append([]string(nil), traceState.redact...)
	traceState.RUnlock()
	if !enabled {
		return
	}

	msg := time.Now().Format("06-01-02 15:04:05.00000 ") + fmt.Sprintf(strings.TrimSpace(pattern)+"\n", args...)
	for _, token := range redact {
		if token != "" {
			msg = strings.ReplaceAll(msg, token, "*****")
		}
	}
	if _, err := out.Write([]byte(msg)); err != nil {
		log.Printf("d1 trace failed: %s", err)
	}
}

func TraceOn(w io.Writer) {
	traceState.Lock()
	defer traceState.Unlock()
	traceState.out = w
	traceState.enabled = true
}

func TraceOff() {
	traceState.Lock()
	defer traceState.Unlock()
	traceState.enabled = false
	traceState.out = io.Discard
}

func addTraceRedaction(s string) {
	if s == "" {
		return
	}
	traceState.Lock()
	defer traceState.Unlock()
	for _, existing := range traceState.redact {
		if existing == s {
			return
		}
	}
	traceState.redact = append(traceState.redact, s)
}
