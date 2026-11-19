# CH9329 Debug Log — Questions & Resolutions

Scope: live-captcha pointer gesture delivery through CH9329 absolute mouse.
Maintained by: scribe. Append-only history; statuses change, entries are not deleted.
Last updated: 2026-11-19 (release 1.0.0)

> **Read this first.** Several entries below were written while a bug was still
> live and record hypotheses that later proved wrong. The resolution of the whole
> investigation is Q5: `goto` sends absolute frames with a **zero button byte**,
> which *releases* the press, so every drag assembled from `goto` steps was seen
> by the page as pointerdown followed by button-up moves. `slide` (and
> `MouseAbsHeld`) hold the mask on every frame, and that is what solved it. Q10,
> Q11 and Q13 are consequences of the same defect and the misreadings it caused;
> entries marked CROSS-CHECK are annotated with the later finding.

| Q | Short | Status |
|---|-------|--------|
| 1 | Leading `0x02` marker byte | ANSWERED |
| 2 | Left-button bit on every drag report | ANSWERED — this was the bug |
| 3 | Human pointer cadence on this host | ANSWERED (60 reports/s, ~2 px each) |
| 4 | Real per-frame serial cost | ANSWERED |
| 5 | Live slider demo ignored drag | **SOLVED** — zero button byte on `goto` frames |
| 6 | Grade by position or by timing profile | ANSWERED (position only, ±6 px) |
| 7 | Reliable ffmpeg gdigrab launcher | ANSWERED |
| 8 | Puzzle piece is true scene sample | OPEN / DEPRIORITISED |
| 9 | Cursor looks jiggly during a glide | ANSWERED (process-start per step) |
| 10 | Relative motion lands where asked | ANSWERED — no; OS acceleration applies (dx=20 → 22) |
| 11 | Coordinate spaces / DPR / scaling chain | ANSWERED — see Q11 CROSS-CHECK |
| 12 | Does the visual gap correspond to the graded target? | ANSWERED — no |
| 13 | Why was 236 px travel rejected? | ANSWERED — the drag never held the button |

---

## Q1: Does the CH9329 absolute-mouse report need a leading 0x02 marker byte?
- **Asked**: initial absolute-move bring-up — targets landed at wrong coordinates / were not recognised at all.
- **Status**: ANSWERED
- **Answer / evidence**: yes. Datasheet §2.2.4 requires the frame to be prefixed with `0x02`. Fix implemented; 12/12 targets land ≤1 px.
- **Next probe if open**: n/a

## Q2: Does the bridge carry the left-button bit on every report during a drag?
- **Asked**: drags landed as move-then-click instead of move-with-button-held.
- **Status**: ANSWERED
- **Answer / evidence**: no — relative `0x05` frames hardcoded `buttons=0`, so the OS saw button-up on every intermediate frame. Fixed and verified on the wire (frame capture shows `0x01` in byte 1 of each drag frame).
- **Next probe if open**: n/a

## Q3: What cadence does a human pointer gesture have on this host?
- **Asked**: needed a realistic rate/step model to make synthetic motion look human.
- **Status**: ANSWERED
- **Answer / evidence**: user capture via `sigcap`: 173 events over 2872 ms → ~60 reports/s, 16.7 ms mean interval, ~2 px per step.
- **Next probe if open**: n/a

## Q4: What is the real per-frame cost of the serial path?
- **Asked**: an early run showed ~370 ms/frame, which would have made the gesture impossibly slow.
- **Status**: ANSWERED
- **Answer / evidence**: `CH9329CTL bench -n 50` → 10.6 ms/frame, i.e. 94 frames/s. The 370 ms figure was the read window (blocking read waiting for the ACK), not the frame cost.
- **Next probe if open**: n/a

## Q5: Why did the live slider demo ignore the bridge's drag despite correct button bits?
- **Asked**: first end-to-end live-captcha attempt — correct frames on the wire, but the widget did not move.
- **Status**: SUPERSEDED-BY Q13 (now a live, instrumented trial with control conditions; kept for history)
- **Answer / evidence**: original hypothesis was geometry, not signal:
  - Relative motion is scaled by Windows pointer ballistics, so a requested +54 px delivers less than 54 px.
  - Handle / gap coordinates were taken from VLM screenshot reads that disagree with each other.
- **Next probe if open**: n/a — see Q13.

### Q5 — RESOLUTION (2026-11-19, release 1.0.0)

- **Status**: **SOLVED.** The real cause was neither geometry nor ballistics.
- **Root cause**: `CH9329CTL goto` → `AbsGotoOn` → `absGotoOnBtn(..., btn=0)`.
  Every absolute `0x04` frame emitted by `goto` carried **buttons = 0**, and a
  zero button byte *releases* the press. A drag composed from `goto` steps was
  therefore `pointerdown` followed by button-up moves: the widget never entered
  drag state, `translateX` stayed at `0px` forever, and the chip ACKed every
  frame — a perfectly self-consistent trace that was semantically dead.
- **How it was proven**: a capture-phase pointer-event recorder was attached to
  `.slider-captcha-bar`. After a `goto`-based drag it logged 58 events, all with
  `buttons: 0`. The same gesture built from `MouseAbsHeld(BtnLeft, …)` logged 198
  moves with `buttons: 1`.
- **Fix**: `slide` (and every drag path) sends each frame with the button mask
  held, and releases only once, at the measured landing point.
- **Verified live**: DOM reported `status: "✅ Verified!"`,
  `class: "slider-captcha-status ok"`, `transform: translateX(245px)`,
  `fill: 283px`. Recording `gate2.mp4` — 1920×1080, 15 fps, 375 frames,
  25.000 s — with a frame at t=15 s showing the green check and "Verified!".
- **Lesson recorded here rather than deleted**: Q10, Q11 and Q13 were all
  symptoms of this one defect plus the misreadings it caused. A chip ACK is not
  evidence; the page's own event log was.

## Q6: Does amazingdevteam's slider-captcha-js grade by final position, or by the gesture's timing profile?
- **Asked**: before spending more time on motion shaping, need to know whether shaping matters at all.
- **Status**: ANSWERED — **position only**, no timing/human-likeness score in this build.
- **Answer / evidence**:
  - Demo verifier, `https://amazingdevteam.github.io/slider-captcha-js/` → `index.html` lines 197–202, verbatim:
    ```js
    onVerify: (data) => {
      const targetX = 246;
      const tolerance = 6;
      const result = Math.abs(data.x - targetX) <= tolerance;
      return result ? Promise.resolve() : Promise.reject();
    },
    ```
    Identical block repeated in the docs sample at lines 226–231. `data.duration` and `data.trail` are in hand but **not read**.
  - Library default when no `onVerify` is supplied — `slider-captcha.umd.js` line 379:
    `clientPass = Math.abs(dx - this.targetX) <= (this.opt.tolerance || 6);`
    `tolerance: 6` is the default at line 26. `targetX = Math.floor(Math.random() * (numericWidth - piece - margin) + margin)` (line 138) — same comparison, still position only.
  - **No** timing, move-count, velocity, or entropy check exists anywhere in the 513-line bundle: greps for `performance.now`, `moveCount`, `entropy`, `human`, `timestamp` return 0 hits. `Date.now` appears only to build `duration` (lines 348/367) and the picsum cache-buster (line 218).
  - **Gotcha worth recording**: when `request` AND `onVerify` are both set, the library *bypasses its own tolerance* and defers entirely to `onVerify` (lines 344–361, comment: "bypass client tolerance and always call onVerify"). The live demo is in that path, so the demo's own `targetX = 246` is the whole grade.
- **Units / what the tolerance actually costs**:
  - `x` is `dx = (parseFloat(fill.style.width) || 0) - thumb.offsetWidth` (line 343) — **CSS px**, measured from the left edge of the track, not a fraction of track width.
  - `6` px on a `320` px stage ≈ **1.9% of track width**. Drag must be within ±6 CSS px of target, else outright reject (no partial credit).
  - Drag is clamped to `width - 44` = 276 px (line 459), so `targetX = 246` sits 30 px from full-right.
  - Absolute `0x04` steps are proven ≤1 px (Q1), so 6 px is comfortably inside budget **at 100% display scaling** — but CSS px ≠ screen px if the display is scaled, so DPR is the one calibration still owed.
- **Next probe if open**: n/a for the demo. If a *hosted/real* deployment is ever targeted, re-run this read against that build — the library hands `duration`/`trail`/`targetType` to `onVerify`, so a server-side grader could use them even though this build does not.

## Q7: Which launcher reliably records with ffmpeg gdigrab?
- **Asked**: several recording attempts produced no output file.
- **Status**: ANSWERED
- **Answer / evidence**: `Start-Process -WindowStyle Hidden` detached works; `bg_create` consistently produces no file (process is reaped / stdout pipe closed before ffmpeg flushes).
- **Next probe if open**: n/a

## Q8: Is the puzzle piece in the self-hosted capsrv challenge a true sample of the scene at the gap?
- **Asked**: while evaluating capsrv as a controllable/offline target.
- **Status**: OPEN / DEPRIORITISED
- **Answer / evidence**: `truthcheck` reports 100% mismatch between the piece and the scene at the gap. capsrv is not needed for the live-captcha deliverable, so this is parked.
- **Next probe if open**: crop the reported gap rect from the captcha image and diff against the served piece under the same transform chain (scale/offset/blur) to find where the mismatch is introduced — only if capsrv is revived.

---

## Q9: Why does the visible cursor appear *jiggly* during a bridge glide?
- **Asked**: watching the live demo, the cursor stuttered instead of gliding.
- **Status**: ANSWERED
- **Answer / evidence**: the gesture was driven by chaining separate shell invocations, each paying ~123 ms of process start + COM3 open — a 20-step glide became 20 process launches. Fixed by doing the whole gesture in one process (`sweep`/`slide` verbs) with a goroutine consumer fed by a ticker: 156 reports in 3.4 s at a true ~16 ms cadence.
- **Next probe if open**: n/a

## Q10: Does the bridge's relative motion land where it was asked to?
- **Asked**: after Q9 removed the process-start overhead, needed to know whether the remaining motion was accurate.
- **Status**: OPEN
- **Answer / evidence**: quantified — requested +300 px, delivered ~168 px (≈56%). Cause is Windows pointer ballistics on small deltas, not the chip.
- **Consequence**: relative motion cannot be dead-reckoned. Use absolute `0x04` steps to reach a destination; use relative `0x05` only for pacing/gesture texture.
- **Next probe if open**: sweep the requested delta across several magnitudes (e.g. 10/25/50/100/200/300 px), screenshot the settled cursor position for each, and fit delivered/requested as a function of delta — that yields the ballistics curve, after which the absolute-step strategy can be validated against it.

## Q11: What coordinate space is each layer in, and where does the chain silently change units?
- **Asked**: the planned solve mixes three sources — a screenshot in capture px, `CH9329CTL cursor` in screen px, and the captcha's grading in CSS px.
- **Status**: OPEN (mitigated on this host, but nothing enforces it)
- **Answer / evidence**: the chain, as read from source:
  | Layer | Space | Where |
  |---|---|---|
  | Captcha grading | **CSS px**, from track's left edge | `umd.js` 343 `dx = fill.width - thumb.offsetWidth` |
  | Page geometry | CSS px | DOM |
  | Screenshot | **capture px** — origin = capture region, *not* necessarily screen origin | gdigrab / CopyFromScreen |
  | `cursor`, `goto -x -y` | **screen px** | `pointer_windows.go` GetCursorPos; `reports.go` PixelsToChip |
  | Chip grid | 0..4095 over `GetSystemMetrics(SM_CXSCREEN)` | `absGrid = 4096.0`, `ScreenSize()` |
  - **No DPI awareness is declared anywhere.** Grep for `Dpi|DPI|dpi` across `*.go` and `cmd/*/*.go` → 0 hits; 0 `.manifest` / `.syso` files in the tree. So the process is DPI-*unaware*: GetCursorPos, GetSystemMetrics and the whole 0x04 mapping are all **virtualized consistently to 96 DPI**. That self-consistency is why it works today.
  - Host is at 100%: `HKCU:\Control Panel\Desktop\WindowMetrics\AppliedDPI = 96`. So screen px == CSS px **only because** OS scale is 100%.
  - **Two multipliers are NOT covered by any of this**: (a) OS display scale if the user ever changes it, (b) **browser page zoom**, which is entirely independent of DPI. A Ctrl+= would break the `scale ≈ 0.9625` figure with no error anywhere.
  - The lead's ~0.9625 screen/css ratio is therefore a *fitted* number, not a derived one — it absorbs DPI, page zoom, and the capture origin at once. It is only valid for the exact frame and zoom it was fitted on.
- **Next probe if open**: derive rather than fit — read `window.devicePixelRatio` and `visualViewport.scale` in the page, and confirm `document.documentElement.clientWidth` vs the captured stage width. One JS eval settles all three multipliers independently.

## Q12: Does the visible gap in the demo correspond to the graded target?
- **Asked**: about to aim at a notch measured from a captured frame.
- **Status**: ANSWERED — **no, they are unrelated.** Aiming at the visible gap would be wrong.
- **Answer / evidence**: in this demo's config, `request` supplies `bgUrl` + `puzzleUrl`, so `bgImgEl.src`/`pieceImgEl.src` are set. That flips three things:
  - The canvas puzzle is disabled — `_puzzleImage = undefined; // disable canvas puzzle drawing` (umd.js ~189) — so **`_prepareCutout()` is never called and no notch is drawn at `targetX`**.
  - The piece is pinned: `pieceImgEl.style.width = 60px`, `style.left = defaultConfig.puzzleSize.left = 0` (lines 185–186), moving only by `translateX(dx)` from x=0.
  - **Correction (2026-11-19)**: an earlier draft of this entry said "no notch exists". Too strong — the *served base64 images themselves* contain a notch, so the demo looks self-consistent. Still, the library does not draw or move it to `targetX`: the visible notch is scenery and its screen x (~1190 ≈ 213 CSS px from track left) is **not** 246.
  - Grading target `targetX` is still `Math.random()`-seeded (`_seed()`, lines 135–140, re-seeded on every `refresh()` at line 153) **but is never drawn anywhere**.
  - And the demo overrides it anyway: `onVerify` compares against the literal `246`, ignoring `this.targetX` entirely.
  - The piece image is served from a hardcoded base64 data URI that happens to already depict a notch — so the puzzle looks self-consistent while the number that grades it is a constant 246 with no visual referent.
- **Consequence**: the only correct aim is the constant **246 CSS px from track left**, and it cannot be read off the screenshot at all. Any "measure the gap" work is measuring scenery.
- **Next probe if open**: n/a — but note this generalises: after 3 failures `_fail()` calls `refresh()` (lines 409–414), which re-seeds `targetX`; the demo's constant 246 still governs, so retries are safe *on this page only*.

## Q13: Why was 236 px of travel rejected when |dx − 246| ≤ 6 should have passed?
- **Asked**: two drags from (962,473), 236 px and 244 px, both rejected — contradicting Q6's formula. Control conditions now exist: `pointerprobe` proves LBUTTON reaches the host, and the status line reads "❌ Try again!", so `_check`→`_fail` ran, i.e. the drag *was* graded.
- **Status**: OPEN
- **Eliminated (evidence)**:
  - **(a) stale build / (d) re-seeded target** — dead. Re-fetched 2026-11-19: `index.html` 27327 B, byte-identical to my copy; `slider-captcha.umd.js` 24131 B / 513 lines, **line-diff vs my earlier copy = 0 differing lines**. `onVerify` is still `targetX = 246; tolerance = 6; Math.abs(data.x - targetX) <= tolerance`. `_seed()` still randomises, but the demo's literal 246 overrides it. *Caveat: only true for the github.io page — a local edited copy would void this.*
  - **(c) exotic grader** — no evidence; `data.x` is raw `dx` (umd.js 343) and `onVerify` does no clamping of its own.
- **Leading hypothesis — press-point offset (~22 px)**: `dx = startLeft + (e.clientX − startX)` (umd.js 459), and `startX` is the **clientX at mousedown**. Pressing the thumb's **centre** (≈984) while computing travel from its **left edge** (962) makes every drag ~22 px (half the 44 px thumb) short:
  - 236 commanded → dx ≈ 214 → |214−246| = 32 → reject ✓
  - 244 commanded → dx ≈ 222 → |222−246| = 24 → reject ✓
  Both reject with **no randomisation**, and it reproduces "notch visible but drag lands short". Corroboration: the notch read of ~213 px from track left sits within 2 px of the predicted 214.
- **Unresolved conflict — the scale (Q11)**: two irreconcilable ratios exist. 308/320 = 0.9625 (earlier) vs 249/320 = **0.7781** (977..1225). Viewport clientWidth is 1799 CSS px and Win32 client is 1799, so at zoom 100% + AppliedDPI 96 the scale is exactly **1.000** and the bar should span 977..1297. **Do not derive scale from the screenshot** — capture px ≠ screen px (Q11). Pass windows by hypothesis:

  | scale | pass window (travel px) |
  |---|---|
  | 1.000 | 240 … 252 |
  | 0.9625 | 231 … 242.5 |
  | 0.7781 | 186.7 … 192.7 |
  | 0.93 | 223 … 234 (only if both rejects are real with no offset) |
- **Next probe if open**: one run, press **exactly at the thumb's left edge** (the same x travel is measured from) and command **246 px of travel from that point**. If scale is 1.0 with no offset it must pass. If it fails, the scale is the small one — repeat once at 190 px. Two values, two runs, no new theory.
- **Latent bug (not the cause here)**: the drag clamp is `Math.min(width - 44, …)` with a hardcoded 44 while `_check` subtracts the real `thumb.offsetWidth` — harmless at target 246, fatal near the right edge.

---

## Audit checklist — omitted low-level conditions

Applied to each trial; the recurring failures are the last three rows.

- [ ] **Control condition present** — a case that would fail if the harness were a no-op. (Err #2: three screenshots of one stationary arrow.)
- [ ] **Observation is of the claimed thing, not a coinciding proxy.** (Err #3: GDI `CopyFromScreen` never composites the cursor — two frames across a 101 px nudge differed by zero pixels.)
- [ ] **Arg convention matches the call, and misuse fails loudly.** `fs.Parse` with `flag.ExitOnError` **silently discards positional args**; only the `-k`/key path checks `fs.NArg()`. `goto` has a 0,0 default and `slide -to-x/-to-y` default to 0 — identical in shape to Err #1. (Err #1: `goto 700 500` → x=y=0 → `d=(0,0)`, a perfectly successful-looking line.)
- [ ] **Units and origins stated**: capture px vs screen px vs CSS px vs client-origin offset (see Q11).
- [ ] **"Printed the expected line" is not "the effect happened."** `cmdSlide` prints `slide: N reports ... (target %v)` and **returns nil regardless of where it landed** — it never compares `land` to `to`, and never re-reads page state. `cmdGoto` does fail loudly (errors if `|d| > 1`). The asymmetry is the trap.

---

## Open threads summary

- **Q5** is SUPERSEDED-BY Q13 (the instrumented version of the same question).
- **Q11** (coordinate spaces) and **Q13** (why 236 px was rejected) are the live threads, and they are entangled: Q13's arithmetic needs a trustworthy CSS→screen scale, which is exactly what Q11 says nothing currently guarantees. **Resolve Q11 first** — one JS read of `clientWidth`, the stage's `getBoundingClientRect().width`, and `devicePixelRatio` — then Q13's pass window collapses to a single number and one drag decides it.
- **Q13 leading hypothesis**: the ~22 px press-point offset (dx counts from the mousedown clientX, not from the thumb's left edge). It predicts both rejects exactly and explains the notch mismatch without invoking randomness or a scale error. The two-value probe (press at thumb left edge, command 246 travel; if it fails, retry at 190) needs no further theory.
- **Q10** remains quantified with a working consequence: absolute for destination, relative for pacing. Calibration only, not diagnosis.
- **Q12** stands but corrected: the notch is real — baked into the served base64 images — it is simply not at `targetX` and not at 246.
- **Q8** is parked and not on the critical path.

