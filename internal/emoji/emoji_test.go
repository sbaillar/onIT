package emoji

import (
	"encoding/base64"
	"testing"
)

func TestAllEmojisProduceValidPayloads(t *testing.T) {
	want := Size * Size * 2
	for _, n := range Names {
		if PNG(n) == nil {
			t.Errorf("%s: missing embedded png", n)
			continue
		}
		p, err := RGB565Base64(n)
		if err != nil {
			t.Errorf("%s: %v", n, err)
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(p)
		if err != nil {
			t.Errorf("%s: payload not valid base64: %v", n, err)
			continue
		}
		if len(raw) != want {
			t.Errorf("%s: payload %d bytes, want %d", n, len(raw), want)
		}
	}
}
