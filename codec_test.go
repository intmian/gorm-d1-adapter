package d1

import (
	"testing"
	"time"
)

func TestCodecTimeBoolBytesRoundTrip(t *testing.T) {
	now := time.Date(2026, 7, 9, 1, 2, 3, 456, time.UTC)
	encodedTime, err := EncodeParam(now)
	if err != nil {
		t.Fatal(err)
	}
	if encodedTime != now.UTC().Format(time.RFC3339Nano) {
		t.Fatalf("unexpected time encoding: %v", encodedTime)
	}

	encodedBool, err := EncodeParam(true)
	if err != nil {
		t.Fatal(err)
	}
	if encodedBool != int64(1) {
		t.Fatalf("unexpected bool encoding: %v", encodedBool)
	}

	raw := []byte{0, 1, 2, 200, 255}
	encodedBytes, err := EncodeParam(raw)
	if err != nil {
		t.Fatal(err)
	}
	decodedBytes, err := DecodeColumnValue("payload", encodedBytes)
	if err != nil {
		t.Fatal(err)
	}
	if string(decodedBytes.([]byte)) != string(raw) {
		t.Fatalf("bytes mismatch: %v", decodedBytes)
	}

	decodedTime, err := DecodeColumnValue("updated_at", encodedTime)
	if err != nil {
		t.Fatal(err)
	}
	if !decodedTime.(time.Time).Equal(now) {
		t.Fatalf("time mismatch: %v", decodedTime)
	}

	decodedBool, err := DecodeColumnValue("flag", float64(1))
	if err != nil {
		t.Fatal(err)
	}
	if decodedBool != true {
		t.Fatalf("bool mismatch: %v", decodedBool)
	}
}
