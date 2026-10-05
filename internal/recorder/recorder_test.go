package recorder

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTriggerDebouncesStartAndGracesStop(t *testing.T) {
	var tr Trigger
	t0 := time.Unix(0, 0)
	at := func(s float64) time.Time { return t0.Add(time.Duration(s * float64(time.Second))) }
	steps := []struct {
		s           float64
		live        bool
		start, stop bool
	}{
		{0, true, false, false},   // call just started: wait out blips
		{3, false, false, false},  // a 3 s blip never records
		{10, true, false, false},  // live again
		{16, true, true, false},   // live for 6 s: start
		{20, true, false, false},  // recording
		{25, false, false, false}, // mic gap: keep recording
		{40, true, false, false},  // back within the grace: same recording
		{45, false, false, false}, // call ends
		{74, false, false, false}, // 29 s later: still in grace
		{76, false, false, true},  // 31 s: stop
		{80, false, false, false}, // stays stopped
	}
	for _, st := range steps {
		start, stop := tr.Update(st.live, at(st.s))
		if start != st.start || stop != st.stop {
			t.Fatalf("t=%vs live=%v: got start=%v stop=%v, want %v %v",
				st.s, st.live, start, stop, st.start, st.stop)
		}
	}
}

func TestTriggerStopsAtOnceWhenForced(t *testing.T) {
	var tr Trigger
	t0 := time.Unix(0, 0)
	tr.Update(true, t0)
	if start, _ := tr.Update(true, t0.Add(6*time.Second)); !start {
		t.Fatal("did not start")
	}
	if _, stop := tr.Reset(); !stop {
		t.Fatal("Reset while recording should report a stop")
	}
	if _, stop := tr.Reset(); stop {
		t.Fatal("Reset while idle should not")
	}
}

func TestResamplerRateAndTone(t *testing.T) {
	const in = 48000
	r := newResampler(in)
	// 1 s of a 440 Hz tone on the left, silence on the right, fed in odd chunks
	src := make([]float32, 2*in)
	for i := 0; i < in; i++ {
		src[2*i] = float32(0.5 * math.Sin(2*math.Pi*440*float64(i)/in))
	}
	var out []int16
	for off := 0; off < len(src); off += 2 * 733 {
		end := min(off+2*733, len(src))
		out = r.process(src[off:end], out)
	}
	frames := len(out) / 2
	if frames < OutRate-2 || frames > OutRate+2 {
		t.Fatalf("got %d frames for 1 s, want ~%d", frames, OutRate)
	}
	var peakL, peakR int16
	for i := 0; i < frames; i++ {
		peakL = max(peakL, out[2*i])
		peakR = max(peakR, out[2*i+1])
	}
	if peakL < 14000 || peakL > 17000 { // 0.5 * 32767, a little lost to the filter
		t.Fatalf("left peak %d, want ~16000", peakL)
	}
	if peakR != 0 {
		t.Fatalf("right channel should be silent, peak %d", peakR)
	}
}

func TestResampler44k(t *testing.T) {
	r := newResampler(44100)
	out := r.process(make([]float32, 2*44100), nil)
	if f := len(out) / 2; f < OutRate-2 || f > OutRate+2 {
		t.Fatalf("got %d frames, want ~%d", f, OutRate)
	}
}

func TestWAVHeaderAfterClose(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.wav")
	w, err := createWAV(p)
	if err != nil {
		t.Fatal(err)
	}
	w.write([]int16{1, -1, 2, -2, 3, -3})
	w.write([]int16{4, -4})
	if err := w.close(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" || string(b[36:40]) != "data" {
		t.Fatalf("bad header %q", b[:44])
	}
	le := binary.LittleEndian
	if ch, rate, bits := le.Uint16(b[22:]), le.Uint32(b[24:]), le.Uint16(b[34:]); ch != 2 || rate != OutRate || bits != 16 {
		t.Fatalf("fmt: ch=%d rate=%d bits=%d", ch, rate, bits)
	}
	if data := le.Uint32(b[40:]); data != 16 || len(b) != 44+16 {
		t.Fatalf("data size %d, file %d", data, len(b))
	}
	if riff := le.Uint32(b[4:]); riff != uint32(len(b)-8) {
		t.Fatalf("riff size %d, want %d", riff, len(b)-8)
	}
}
