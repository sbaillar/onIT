package busylight

import (
	"log"
	"strings"
	"sync"
	"time"

	"go.bug.st/serial"
	"go.bug.st/serial/enumerator"
)

const baud = 115200

// USB vendor IDs to match: Espressif native USB, WCH CH34x bridge.
var usbVIDs = map[string]bool{"303A": true, "1A86": true}

// Light owns the serial port. Reconnects lazily on every send.
type Light struct {
	mu   sync.Mutex
	port serial.Port
	Cmds chan string // CMD payloads read from the device
}

func NewLight() *Light {
	return &Light{Cmds: make(chan string, 8)}
}

func findPort() string {
	ports, err := enumerator.GetDetailedPortsList()
	if err != nil {
		log.Printf("port enumeration failed: %v", err)
		return ""
	}
	for _, p := range ports {
		if p.IsUSB && usbVIDs[strings.ToUpper(p.VID)] {
			return p.Name
		}
	}
	return ""
}

// ListPorts logs all serial ports (the -ports flag).
func ListPorts() {
	ports, err := enumerator.GetDetailedPortsList()
	if err != nil {
		log.Fatal(err)
	}
	for _, p := range ports {
		if p.IsUSB {
			log.Printf("%s  VID:%s PID:%s  %s", p.Name, p.VID, p.PID, p.Product)
		} else {
			log.Print(p.Name)
		}
	}
}

// ensureLocked opens the port and starts a reader goroutine. Caller holds mu.
func (l *Light) ensureLocked() bool {
	if l.port != nil {
		return true
	}
	name := findPort()
	if name == "" {
		return false
	}
	port, err := serial.Open(name, &serial.Mode{BaudRate: baud})
	if err != nil {
		log.Printf("open %s failed: %v", name, err)
		return false
	}
	time.Sleep(500 * time.Millisecond) // board may reset on open
	l.port = port
	log.Printf("Serial connected: %s", name)
	go l.reader(port)
	return true
}

// reader pushes CMD lines from the device onto l.Cmds until the port dies.
func (l *Light) reader(port serial.Port) {
	buf := make([]byte, 256)
	var line []byte
	for {
		n, err := port.Read(buf)
		if err != nil {
			l.drop(port)
			return
		}
		for _, b := range buf[:n] {
			if b != '\n' {
				line = append(line, b)
				continue
			}
			s := strings.TrimSpace(string(line))
			line = line[:0]
			if cmd, ok := strings.CutPrefix(s, "CMD:"); ok {
				select {
				case l.Cmds <- cmd:
				default:
				}
			}
		}
	}
}

// drop closes port if it is still the active one.
func (l *Light) drop(port serial.Port) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.port == port {
		l.port.Close()
		l.port = nil
	}
}

// Send writes a state to the device. Returns false if no device is connected.
func (l *Light) Send(state string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.ensureLocked() {
		return false
	}
	if _, err := l.port.Write([]byte("STATE:" + state + "\n")); err != nil {
		l.port.Close()
		l.port = nil
		return false
	}
	return true
}

// Connected reports whether a serial port is currently open.
func (l *Light) Connected() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.port != nil
}
