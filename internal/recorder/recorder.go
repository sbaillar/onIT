// Package recorder captures Teams meetings for transcription: the microphone
// on the left channel and everything the Mac plays (the other people) on the
// right, as a 16 kHz stereo WAV that meetnote picks up.
package recorder

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// OutRate is the WAV sample rate: all Whisper and the speaker models need.
const OutRate = 16000

// ErrUnsupported is returned where there is no capture backend.
var ErrUnsupported = errors.New("meeting recording is only supported on macOS 14.2+")

const (
	startAfter = 5 * time.Second  // a call must stay live this long to start a recording
	stopAfter  = 30 * time.Second // and stay down this long to end it
)

// Trigger turns the "in a call" signal into recording start/stop edges: it
// ignores blips under startAfter and keeps one recording through gaps under
// stopAfter (a muted stretch, a headset switch).
type Trigger struct {
	recording bool
	since     time.Time // when live last changed to the value that matters
	live      bool
}

// Update feeds the current signal; start or stop is true on the edge.
func (t *Trigger) Update(live bool, now time.Time) (start, stop bool) {
	if live != t.live || t.since.IsZero() {
		t.live, t.since = live, now
	}
	held := now.Sub(t.since)
	switch {
	case !t.recording && live && held >= startAfter:
		t.recording = true
		return true, false
	case t.recording && !live && held >= stopAfter:
		t.recording = false
		return false, true
	}
	return false, false
}

// Reset forgets the state; stop is true if a recording was running.
func (t *Trigger) Reset() (start, stop bool) {
	was := t.recording
	*t = Trigger{}
	return false, was
}

// resampler turns interleaved stereo float32 at the device rate into
// interleaved int16 at OutRate: a box low-pass the width of one output
// sample, then linear interpolation. Plenty for speech.
type resampler struct {
	step  float64   // input frames per output frame
	pos   float64   // next output position, in input frames since start
	base  int64     // input frame index of hist[0]
	hist  []float32 // filtered input frames not yet consumed (interleaved)
	box   int
	raw   []float32 // last box-1 raw frames, for the moving average
	sumL  float64
	sumR  float64
	count int
}

func newResampler(inRate float64) *resampler {
	box := max(1, int(inRate/OutRate+0.5))
	return &resampler{step: inRate / OutRate, box: box}
}

func (r *resampler) process(in []float32, out []int16) []int16 {
	for i := 0; i+1 < len(in); i += 2 {
		// moving average over the last box frames
		r.raw = append(r.raw, in[i], in[i+1])
		r.sumL += float64(in[i])
		r.sumR += float64(in[i+1])
		if len(r.raw) > 2*r.box {
			r.sumL -= float64(r.raw[0])
			r.sumR -= float64(r.raw[1])
			r.raw = r.raw[2:]
		}
		n := float64(len(r.raw) / 2)
		r.hist = append(r.hist, float32(r.sumL/n), float32(r.sumR/n))
	}
	frames := int64(len(r.hist) / 2)
	for r.pos+1 < float64(r.base+frames) {
		k := int64(r.pos) - r.base
		f := float32(r.pos - float64(int64(r.pos)))
		l := r.hist[2*k]*(1-f) + r.hist[2*k+2]*f
		rr := r.hist[2*k+1]*(1-f) + r.hist[2*k+3]*f
		out = append(out, toInt16(l), toInt16(rr))
		r.pos += r.step
	}
	// drop consumed frames, keep the one interpolation still needs
	if drop := int64(r.pos) - r.base; drop > 0 {
		drop = min(drop, frames)
		r.hist = r.hist[2*drop:]
		r.base += drop
	}
	return out
}

func toInt16(v float32) int16 {
	v *= 32767
	if v > 32767 {
		return 32767
	}
	if v < -32768 {
		return -32768
	}
	return int16(v)
}

// wavWriter streams 16-bit stereo PCM and fills in the sizes on close.
type wavWriter struct {
	f    *os.File
	data uint32
}

func createWAV(path string) (*wavWriter, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	w := &wavWriter{f: f}
	if _, err := f.Write(w.header()); err != nil {
		f.Close()
		return nil, err
	}
	return w, nil
}

func (w *wavWriter) header() []byte {
	const ch, bits = 2, 16
	h := make([]byte, 44)
	le := binary.LittleEndian
	copy(h[0:], "RIFF")
	le.PutUint32(h[4:], 36+w.data)
	copy(h[8:], "WAVEfmt ")
	le.PutUint32(h[16:], 16)
	le.PutUint16(h[20:], 1) // PCM
	le.PutUint16(h[22:], ch)
	le.PutUint32(h[24:], OutRate)
	le.PutUint32(h[28:], OutRate*ch*bits/8)
	le.PutUint16(h[32:], ch*bits/8)
	le.PutUint16(h[34:], bits)
	copy(h[36:], "data")
	le.PutUint32(h[40:], w.data)
	return h
}

func (w *wavWriter) write(s []int16) error {
	if err := binary.Write(w.f, binary.LittleEndian, s); err != nil {
		return err
	}
	w.data += uint32(2 * len(s))
	return nil
}

func (w *wavWriter) close() error {
	if _, err := w.f.WriteAt(w.header(), 0); err != nil {
		w.f.Close()
		return err
	}
	return w.f.Close()
}

// Recording is one meeting being written. It goes to <name>.wav.part and is
// renamed to <name>.wav on Stop, so watchers never pick up a half file.
type Recording struct {
	path string
	stop chan struct{}
	done chan error
	once sync.Once
}

// Start begins capturing into dir/meeting-<time>.wav.
func Start(dir string) (*Recording, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "meeting-"+time.Now().Format("2006-01-02-1504")+".wav")
	rate, err := captureStart()
	if err != nil {
		return nil, err
	}
	w, err := createWAV(path + ".part")
	if err != nil {
		captureStop()
		return nil, err
	}
	r := &Recording{path: path, stop: make(chan struct{}), done: make(chan error, 1)}
	go r.pump(w, newResampler(rate))
	return r, nil
}

// pump drains the capture buffer into the WAV until Stop.
func (r *Recording) pump(w *wavWriter, rs *resampler) {
	buf := make([]float32, 2*48000) // up to 1 s of stereo at 48 kHz per read
	var out []int16
	var werr error
	drain := func() {
		for {
			n := captureRead(buf)
			if n == 0 {
				return
			}
			out = rs.process(buf[:2*n], out[:0])
			if werr == nil {
				werr = w.write(out)
			}
		}
	}
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
			drain()
		case <-r.stop:
			captureStop()
			drain()
			err := w.close()
			if werr != nil {
				err = werr
			}
			if err == nil {
				err = os.Rename(r.path+".part", r.path)
			}
			if d := captureDropped(); d > 0 && err == nil {
				err = fmt.Errorf("recording kept, but %d capture buffers were dropped", d)
			}
			r.done <- err
			return
		}
	}
}

// Stop ends the recording and returns the finished file.
func (r *Recording) Stop() (string, error) {
	r.once.Do(func() { close(r.stop) })
	return r.path, <-r.done
}
