# CH9329CTL 1.0.0 — release notes

## What this is

A command-line driver for a CH9329 serial-to-HID bridge, on Windows. It moves
the pointer and types on a target machine as if from a real USB keyboard and
mouse, and it proves what happened by reading the pointer back in-process.

## Highlights of this release

### The held-gesture primitive (`slide`)

`slide` performs one continuous press → travel → release, sending every frame of
the path with the button mask held and releasing at the *measured* landing point.
It exists because the obvious composition does not work, for a reason that is
worth stating plainly:

> An absolute `0x04` report with a zero button byte **releases** the button. A
> drag assembled from plain `goto` frames is therefore `pointerdown` followed by
> button-up moves. A slider handle never enters drag state, `translateX` stays at
> `0px`, and the chip ACKs every single frame of the futile gesture — a
> perfectly self-consistent trace that is completely wrong.

Verified end-to-end against a live slider CAPTCHA using HID frames only: the
page's own DOM reported `status: "✅ Verified!"`, `translateX(245px)`, a filled
bar, and 198 held-button pointer events.

### Human-shaped motion (`-human`)

Pointer motion can now be shaped like a hand: lateral tremor, jittered frame
timing, easy-in/out pacing, incidental pauses, and a slightly non-straight path.

The constraint that makes this safe:

> Jitter perturbs the **path**, never the **landing point**. The tremor envelope
> is zero at both ends and the final frame is an exact absolute report, so a
> graded destination is unaffected by the randomness.

Measured on a 246 px travel: 25 reports plain vs. 75 reports over ~2.9 s with
`-human`, both releasing at the same coordinate. New knobs: `-jitter`, `-ease`,
`-timing-sigma`, `-no-jitter`, `-seed`.

### Reliability fixes

- **Port reopen loop.** The CH343 adapter failed `serial.Open` with
  `Serial port busy` on ~50 % of attempts and swallowed the first frame after
  opening, silently dropping button frames mid-gesture. Opens now succeed 10/10
  under the retry loop.
- **`goto` coordinates are flags.** `goto 700 500` is silently ignored by Go's
  flag parser and lands at `(0,0)`. Documented in `help`, in the README, and in
  `QUESTIONS.md`.
- **Absolute glide convergence.** `Drag` now ends on an exact absolute frame
  carrying the button mask, instead of releasing from wherever the path
  happened to stop.

## Cleanup

~130 session artifacts (screenshots, raw captures, recordings, ad-hoc probes)
were quarantined to `_scratch/`; six dead commands were removed from `cmd/`;
a `//go:embed` target that broke the build was identified and its dead consumer
removed. The build tree is now the library, three commands, and the docs.

```
go build ./...   ok
go vet ./...     ok
go test ./...    ok
selftest         PASS (GET_INFO, 4 absolute targets, smooth glide, relative)
```

## Known limitations

- Windows only: the pointer witness is Win32 `GetCursorPos`.
- Absolute accuracy is bounded by the chip's grid: ~0.47 px horizontal and
  ~0.26 px vertical on a 1920×1080 screen. Reported residuals of ±1 px are that
  quantisation, not error.
- `-human` changes appearance, not correctness. It is cosmetic by design.
- The pointer cannot be located from a screenshot on this class of host: GDI
  screen grabs do not composite the cursor. Use `cursor`.

## Upgrading

Existing invocations keep working; `-human` is opt-in and defaults off. The one
behavioural change to be aware of is that `drag` now lands on an exact absolute
frame at the end of its path rather than releasing wherever the glide stopped.
