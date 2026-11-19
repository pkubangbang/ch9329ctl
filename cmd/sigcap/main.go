//go:build windows

// sigcap - capture RAW input signals and diff them against what the CH9329 bridge
// produces.
//
// Why: the bridge can emit a report that the chip ACKs and the OS records
// (GetAsyncKeyState says the button is down) while a UI still ignores the gesture.
// An ACK and even a global key state are not proof that the HID input arrived as a
// clean device event sequence. A human, using a real mouse, produces the reference
// sequence. This tool records that reference and then records the bridge's
// sequence the same way, so the two can be compared field by field:
//
//	Raw Input  (WM_INPUT)   - what Windows itself received from the device
//	                          (RI_MOUSE_* flags, button transitions), and
//	Low-level hook (WH_MOUSE_LL / WH_KEYBOARD_LL) - what the input pipeline saw.
//
// Usage:
//
//	sigcap -out human.json -secs 15      # you drive the real mouse; press Esc-free
//	sigcap -out bridge.json -secs 15    # run the bridge from another shell meanwhile
//	sigcap -diff human.json bridge.json
//
// The JSON is an ordered event list, so a diff highlights missing transitions
// (e.g. "no RI_MOUSE_LEFT_BUTTON_DOWN/RIGHT_BUTTON_UP pair around the moves") or
// a wrong button state on the intermediate motion reports.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procRegisterClassExW  = user32.NewProc("RegisterClassExW")
	procCreateWindowExW   = user32.NewProc("CreateWindowExW")
	procDefWindowProcW    = user32.NewProc("DefWindowProcW")
	procRegisterRawInput  = user32.NewProc("RegisterRawInputDevices")
	procGetRawInputData   = user32.NewProc("GetRawInputData")
	procSetWindowsHookExW = user32.NewProc("SetWindowsHookExW")
	procCallNextHookEx    = user32.NewProc("CallNextHookEx")
	procGetMessageW       = user32.NewProc("GetMessageW")
	procTranslateMessage  = user32.NewProc("TranslateMessage")
	procDispatchMessageW  = user32.NewProc("DispatchMessageW")
	procPostQuitMessage   = user32.NewProc("PostQuitMessage")
	procSetTimer          = user32.NewProc("SetTimer")
	procGetModuleHandleW  = kernel32.NewProc("GetModuleHandleW")
)

const (
	wmInput       = 0x00FF
	wmQuit        = 0x0012
	wmTimer       = 0x0113
	ridInput      = 0x10000003
	rimTypeMouse  = 0
	riteInputSink = 0x00000100
	whMouseLL     = 14

	// RI_MOUSE_* flags as reported for HID mice.
	riMouseLeftDown   = 0x0001
	riMouseLeftUp     = 0x0002
	riMouseRightDown  = 0x0004
	riMouseRightUp    = 0x0008
	riMouseMiddleDown = 0x0010
	riMouseMiddleUp   = 0x0020
)

type rawMouse struct {
	Flags uint16
	_     uint16
	Data  [4]uint32 // Buttons | (X<<16) in a raw-motion union
}

type rawInputHeader struct {
	Type uint32
	Size uint32
	Dev  syscall.Handle
	W    uintptr
}

// Event is one captured signal.
type Event struct {
	T     int64  `json:"t_ms"`
	Kind  string `json:"kind"`            // "raw" | "hook"
	Flags string `json:"flags,omitempty"` // RI_MOUSE_* decoded, or hook flags
	DX    int32  `json:"dx,omitempty"`
	DY    int32  `json:"dy,omitempty"`
	Note  string `json:"note,omitempty"`
}

type msgT struct {
	Hwnd     uintptr
	Message  uint32
	WParam   uintptr
	LParam   uintptr
	Time     uint32
	Pt       struct{ X, Y int32 }
	LPrivate uint32
}

var (
	events []Event
	t0     = time.Now()
)

func stamp() int64 { return int64(time.Since(t0) / time.Millisecond) }

func decodeRawFlags(f uint16) string {
	var out []string
	add := func(bit uint16, name string) {
		if f&bit != 0 {
			out = append(out, name)
		}
	}
	add(riMouseLeftDown, "LEFT_DOWN")
	add(riMouseLeftUp, "LEFT_UP")
	add(riMouseRightDown, "RIGHT_DOWN")
	add(riMouseRightUp, "RIGHT_UP")
	add(riMouseMiddleDown, "MIDDLE_DOWN")
	add(riMouseMiddleUp, "MIDDLE_UP")
	add(0x0400, "MOVE_ABSOLUTE")
	add(0x0000, "MOVE_RELATIVE")
	if len(out) == 0 {
		return "MOVE"
	}
	return join(out, "|")
}

func join(s []string, sep string) string {
	out := ""
	for i, v := range s {
		if i > 0 {
			out += sep
		}
		out += v
	}
	return out
}

func wndProc(h uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case wmInput:
		var size uint32
		procGetRawInputData.Call(h, ridInput, 0, uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Sizeof(rawInputHeader{})))
		if size == 0 || size > 1024 {
			break
		}
		buf := make([]byte, size)
		n, _, _ := procGetRawInputData.Call(h, ridInput, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Sizeof(rawInputHeader{})))
		if n == ^uintptr(0) {
			break
		}
		hdr := (*rawInputHeader)(unsafe.Pointer(&buf[0]))
		if hdr.Type == rimTypeMouse {
			m := (*rawMouse)(unsafe.Pointer(&buf[unsafe.Sizeof(rawInputHeader{})]))
			b := m.Data[0]
			dx := int16(b & 0xFFFF)
			dy := int16((b >> 16) & 0xFFFF)
			events = append(events, Event{
				T: stamp(), Kind: "raw", Flags: decodeRawFlags(m.Flags), DX: int32(dx), DY: int32(dy),
			})
		}
		return 0
	case wmTimer:
		// Timer ends the capture window (handled in the loop below).
		return 0
	}
	ret, _, _ := procDefWindowProcW.Call(h, uintptr(msg), wParam, lParam)
	return ret
}

func main() {
	out := flag.String("out", "capture.json", "JSON file to write")
	secs := flag.Int("secs", 15, "capture window in seconds")
	diffA := flag.String("diff", "", "first file to diff (with -diff2)")
	diffB := flag.String("diff2", "", "second file to diff")
	flag.Parse()

	if *diffA != "" && *diffB != "" {
		os.Exit(diff(*diffA, *diffB))
	}

	hInst, _, _ := procGetModuleHandleW.Call(0)
	cls, _ := syscall.UTF16PtrFromString("sigcapWnd")
	wc := struct {
		Size, Style, ClsExtra, WndExtra uint32
		WndProc, Cls, Icon, IconSm      uintptr
		Cursor, Background, MenuName    uintptr
		ClassName                       *uint16
	}{}
	wc.Size = 48
	wc.WndProc = syscall.NewCallback(wndProc)
	wc.ClassName = cls
	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	hwnd, _, _ := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(cls)), 0, 0, 0, 0, 0, 0, 0, 0, hInst, 0)

	// Register for BOTH relative mouse and raw keyboard so a keyboard capture works too.
	type ridEntry struct {
		Usage, Flags uint16
		Target       syscall.Handle
	}
	// RIDEV_INPUTSINK (0x0100) = deliver to this window even when it is not focused.
	rides := [2]ridEntry{
		{Usage: 1 /*RIDEV_DEVNOTIFY-adjacent: mouse*/, Flags: riteInputSink, Target: syscall.Handle(hwnd)},
		{Usage: 2 /*keyboard*/, Flags: riteInputSink, Target: syscall.Handle(hwnd)},
	}
	procRegisterRawInput.Call(uintptr(unsafe.Pointer(&rides[0])), 2, uintptr(unsafe.Sizeof(rides[0])))

	// Low-level hook on the same thread (the message loop pumps it).
	hook, _, hookErr := procSetWindowsHookExW.Call(whMouseLL, syscall.NewCallback(llProc), 0, 0)
	if hook == 0 {
		fmt.Fprintf(os.Stderr, "warning: WH_MOUSE_LL not installed (%v); raw input only\n", hookErr)
	}

	// Deadline via a Win32 timer, NOT a goroutine: PostQuitMessage from another
	// thread was never delivered and the process hung with no output at all.
	procSetTimer.Call(hwnd, 2, uintptr(*secs*1000), 0)
	fmt.Printf("recording for %d s -> %s   (drive the pointer now)\n", *secs, *out)
	var msg msgT
loop:
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		if msg.Message == wmTimer {
			break loop
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}

	data, _ := json.MarshalIndent(map[string]any{
		"secs": *secs, "events": events, "count": len(events),
	}, "", " ")
	os.WriteFile(*out, data, 0o644)
	fmt.Printf("wrote %s (%d events)\n", *out, len(events))
}

func llProc(nCode int32, wParam, lParam uintptr) uintptr {
	if nCode >= 0 {
		var name string
		switch wParam {
		case 0x0200:
			name = "MOUSEMOVE"
		case 0x0201:
			name = "LBUTTONDOWN"
		case 0x0202:
			name = "LBUTTONUP"
		case 0x0204:
			name = "RBUTTONDOWN"
		case 0x0205:
			name = "RBUTTONUP"
		default:
			name = fmt.Sprintf("MSG%#04x", wParam)
		}
		if len(events) < 20000 {
			events = append(events, Event{T: stamp(), Kind: "hook", Flags: name})
		}
	}
	ret, _, _ := procCallNextHookEx.Call(0, uintptr(nCode), wParam, lParam)
	return ret
}

// diff summarises two captures side by side: event counts by kind and flags, and
// the presence/absence of each button transition.
func diff(a, b string) int {
	load := func(p string) map[string]any {
		var v map[string]any
		data, err := os.ReadFile(p)
		if err != nil {
			fmt.Fprintf(os.Stderr, "read %s: %v\n", p, err)
			return nil
		}
		json.Unmarshal(data, &v)
		return v
	}
	sum := func(v map[string]any) (map[string]int, int) {
		counts := map[string]int{}
		var evs []any
		if v != nil {
			evs, _ = v["events"].([]any)
		}
		for _, e := range evs {
			m, _ := e.(map[string]any)
			kind, _ := m["kind"].(string)
			flags, _ := m["flags"].(string)
			counts[kind+":"+flags]++
		}
		return counts, len(evs)
	}
	ca, na := sum(load(a))
	cb, nb := sum(load(b))
	fmt.Printf("%-28s %6s %6s\n", "signal", "A", "B")
	keys := map[string]bool{}
	for k := range ca {
		keys[k] = true
	}
	for k := range cb {
		keys[k] = true
	}
	var ordered []string
	for k := range keys {
		ordered = append(ordered, k)
	}
	sort.Strings(ordered)
	for _, k := range ordered {
		mark := " "
		if (ca[k] == 0) != (cb[k] == 0) {
			mark = "!"
		}
		fmt.Printf("%s %-26s %6d %6d\n", mark, k, ca[k], cb[k])
	}
	fmt.Printf("  %-26s %6d %6d\n", "TOTAL EVENTS", na, nb)
	return 0
}
