# Porting CH9329CTL to another OS

Windows is implemented. Linux and macOS **compile and run**, but their pointer
witness is stubbed, so any command that would move the pointer fails loudly:

```
ERROR: ch9329: pointer witness is not implemented on this platform (see platform_other.go)
```

That is deliberate. A driver that cannot see the pointer cannot prove that
anything it sent had an effect — and silently pretending a move worked is the
exact failure mode this project exists to eliminate (see
[`QUESTIONS.md`](QUESTIONS.md), Q5).

## What is already portable

Almost everything. These files have no OS dependency and need no changes:

| File | Contents |
|---|---|
| `protocol.go` | CH9329 framing: GET_INFO, `0x04` absolute, `0x05` relative, key reports |
| `reports.go` | Report byte layout, button/modifier masks, `PixelsToChip` |
| `keymap.go` | Key-spec parsing → HID usage codes |
| `human.go` | Human-shaped motion: tremor, timing jitter, easing |
| `ctl.go` | Pointer control: goto, glide, drag, slide, repeat |
| `device.go`, `port.go` | Port open with retry, handshake, frame send/reply |
| `hex.go`, `raw.go` | Tracing helpers |

The serial layer uses `go.bug.st/serial`, which is already cross-platform. The
CH9329 speaks the same protocol regardless of the host OS.

## What you must implement

One file: `platform_other.go` (built for `!windows`). Replace its stubs with a
real implementation — or split it into `platform_linux.go` /
`platform_darwin.go` and keep the stubs as the fallback.

### The contract

| Symbol | Requirement |
|---|---|
| `CursorPoint() (x, y int, err error)` | The **real** pointer position, in the same coordinate space `ScreenSize` reports, readable **in the same process that sent the frame**. This is the feedback witness; everything else is secondary. Never return a cached or requested value. |
| `ScreenSize() (w, h int)` | The full coordinate space the `0x04` grid maps onto — the whole virtual desktop if the OS composes monitors into one space. |
| `SetCursor(x, y int)` | Used **only** by `selftest` as a control condition: it moves the pointer *without* the chip, to prove the witness works at all. Use the native API, not frames. |
| `ButtonState(vk uintptr)` | Whether a virtual key is currently down. Separates "the press never reached the host" from "the press landed somewhere that ignores it". VK codes: left `0x01`, right `0x02`, middle `0x04`. |
| Window helpers | `ListWindows`, `FindWindow`, `Foreground`, `Focus`, `Restore`, `ClientOrigin`, `ClientSize`, `WindowRect`, `TitleContains`. These back the `win` and `focus` commands. Stubbing them degrades usability, not correctness — **except** `Focus`, which must report failure rather than claim success. |

### X11

The straightforward mapping:

| ch9329 | Xlib |
|---|---|
| `CursorPoint` | `XQueryPointer` |
| `ScreenSize` | `XDisplayWidth` / `XDisplayHeight` |
| `SetCursor` | `XWarpPointer` |
| `ButtonState` | `XQueryPointer` mask bits |
| `Focus` | `XRaiseWindow` + `_NET_ACTIVE_WINDOW` EWMH message |
| `ListWindows` | `XQueryTree` recursively, or `_NET_CLIENT_LIST` |

Pure-Go options: [`github.com/jezek/xgb`](https://github.com/jezek/xgb)
(no cgo, preferred) or `XOpenDisplay` via cgo with `-lX11`.

**Careful with XWayland.** Under XWayland, `XQueryPointer` reports the *X
server's* idea of the pointer, which the Wayland compositor may not honour. The
witness must come from the same input stack that receives the injected HID
reports, or the readback is meaningless. This is the same class of bug as
reading the cursor from a different process — it looks fine and proves nothing.

### Wayland — read this before you start

Wayland deliberately denies clients the ability to read the global pointer and
to warp it: for a Wayland client there is no global cursor position, and no
`XWarpPointer` equivalent.

Note the asymmetry, though: **a CH9329 is not a client.** It is a hardware USB
device, so the compositor delivers its events exactly as it would a real mouse —
injection *works*. Reading the position back is the hard half:

- **libinput / `/dev/input`** gives you event *deltas*, not absolute position,
  and needs privileges. You would have to integrate deltas yourself, which
  reintroduces accumulated error — precisely what the absolute `0x04` path on
  Windows avoids.
- **Compositor-specific queries** (KDE/KWin D-Bus, GNOME/Mutter portals) exist
  but are not uniform, and the `RemoteDesktop` portal is designed for
  *interactive* consent, which defeats headless automation.
- **A virtual/absolute pointer device** (uinput with `ABS_X`/`ABS_Y`) is the
  most promising route: if the host maps the CH9329 as an absolute pointer, the
  compositor's own notion of position becomes queryable via the same uinput
  node you created.

Treat the witness as the central design question of the Linux port, not a
detail. Everything in `ctl.go` assumes a trustworthy `CursorPoint`.

### macOS

- `CursorPoint` → `CGEventGetLocation(CGEventCreate(nil))` or
  `CGDisplayBounds` + `NSEvent.mouseLocation` (note AppKit's flipped Y axis).
- `SetCursor` → `CGWarpMouseCursorPosition`.
- `ScreenSize` → `CGDisplayBounds(CGMainDisplayID())`.
- Requires the **Accessibility** permission for input injection; the process
  must be trusted (`AXIsProcessTrustedWithOptions`). Without it, injection
  silently does nothing — check it and fail loudly.
- `ButtonState` → `CGEventSourceButtonState`.

## Verifying a port

Do not trust a build; run the checks in order.

```bash
go build ./...
go vet ./...
go test ./...          # frame layout + motion shaper tests are OS-independent
go run ./cmd/ch9329ctl version
go run ./cmd/ch9329ctl selftest
```

`selftest` is the gate. It proves the feedback channel **before** trusting it:

1. A local move (`SetCursor`, no chip) must read back. If it does not, it exits
   **3 — METHOD INVALID**: the witness is broken and nothing the chip did can be
   judged.
2. `GET_INFO` must return a valid chip reply.
3. Four absolute targets must land within ±1 px.
4. A smooth glide must travel in several steps and land within 1.5 px.
5. A relative nudge must actually move the pointer.

Only when `selftest` prints `SELFTEST PASS` is the port trustworthy.

## Definition of done for a new platform

- [ ] `CursorPoint` returns the real position, same-process, same coordinate space.
- [ ] `selftest` prints `SELFTEST PASS` and never reaches step 2 with a broken witness.
- [ ] Unimplemented pieces return `ErrUnsupported` — never a zero value that looks like success.
- [ ] `README.md`'s platform section is updated honestly, including what does *not* work.
- [ ] `go build`, `go vet`, `go test` clean for both the new GOOS and `windows`.
