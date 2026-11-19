//go:build windows

// cmd/pointerprobe - does the CH9329's *button state* reach this host?
//
// The question this answers, and why it needs its own program:
//
//	The bridge moves the pointer perfectly (absolute 0x04, d=(0,0) every
//	time) but a press-and-drag over a real application changes nothing. Two
//	very different causes fit that:
//
//	  (a) the button bits never reach the host, so no mousedown is ever
//	      delivered and every gesture is a hover; or
//	  (b) the press lands somewhere that does not listen.
//
//	Asking the page cannot separate them, because "nothing happened" is the
//	expected result of both. The host can: GetAsyncKeyState(VK_LBUTTON) tells
//	us whether Windows thinks the physical button is down, in the same process
//	that sent the frame, with no application involved.
//
// Usage:
//
//	pointerprobe -x 1101 -y 473 -hold 1200ms
//	pointerprobe -probe            (report button state without changing it)
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"ch9329"
)

const (
	vkLBUTTON = 0x01
	vkRBUTTON = 0x02
	vkMBUTTON = 0x04
)

func asyncDown(vk int) bool {
	// GetAsyncKeyState's high bit = currently down.
	v, _, _ := ch9329.ButtonState(uintptr(vk))
	return int16(v) < 0
}

func report(what string) {
	fmt.Printf("%-22s LBUTTON=%-5v RBUTTON=%-5v MBUTTON=%v\n", what,
		asyncDown(vkLBUTTON), asyncDown(vkRBUTTON), asyncDown(vkMBUTTON))
}

func main() {
	x := flag.Int("x", -1, "pointer x")
	y := flag.Int("y", -1, "pointer y")
	hold := flag.Duration("hold", 1200*time.Millisecond, "how long to hold the button")
	probe := flag.Bool("probe", false, "only report the current button state")
	port := flag.String("port", "COM3", "serial port")
	baud := flag.Int("baud", 115200, "baud rate")
	flag.Parse()

	if *probe {
		report("probe")
		return
	}
	if *x < 0 || *y < 0 {
		fmt.Fprintln(os.Stderr, "need -x and -y (or -probe)")
		os.Exit(2)
	}

	d, err := ch9329.OpenDevice(*port, *baud)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open: %v\n", err)
		os.Exit(1)
	}
	defer d.Close()

	w, h := ch9329.ScreenSize()
	p := ch9329.Point{X: *x, Y: *y}

	report("before move")
	if _, err := d.AbsGoto(p, 300*time.Millisecond); err != nil {
		fmt.Fprintf(os.Stderr, "goto: %v\n", err)
		os.Exit(1)
	}
	report("after move")

	gx, gy := ch9329.PixelsToChip(p.X, p.Y, w, h)
	if err := d.MouseDown(ch9329.BtnLeft, gx, gy); err != nil {
		fmt.Fprintf(os.Stderr, "down: %v\n", err)
		os.Exit(1)
	}
	// Sample the host's view while the button is held. Give the host a few
	// ms to deliver the report first -- a report that arrives after we look
	// would make a working bridge look broken.
	time.Sleep(60 * time.Millisecond)
	report("while held")

	// Hold, nudging so the app sees motion with the button down.
	deadline := time.Now().Add(*hold)
	for time.Now().Before(deadline) {
		time.Sleep(80 * time.Millisecond)
		if _, err := d.MouseAbsHeld(ch9329.BtnLeft, gx, gy); err != nil {
			fmt.Fprintf(os.Stderr, "held: %v\n", err)
			break
		}
	}

	if err := d.MouseUp(gx, gy); err != nil {
		fmt.Fprintf(os.Stderr, "up: %v\n", err)
		os.Exit(1)
	}
	time.Sleep(60 * time.Millisecond)
	report("after release")

	cx, cy, _ := ch9329.CursorPoint()
	fmt.Printf("cursor=%d,%d (wanted %d,%d)\n", cx, cy, p.X, p.Y)
}
