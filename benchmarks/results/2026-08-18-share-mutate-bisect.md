# bench_share_mutate bisect — +7% regression on perf/vm-dispatch-fase1

Date: 2026-08-18
Scope: measurement only, no production code changed.

## Question

Which commit on `perf/vm-dispatch-fase1` introduced the +7.0% regression on
`bench_share_mutate` (candidate vs `develop` baseline `f107508`), or is it
noise?

## Method

- Isolated detached worktree in scratchpad
  (`.../scratchpad/bisect`), never touching the main checkout
  (`D:\OneDrive\Documentos\go_projects\noxy`, which stayed on branch
  `perf/vm-dispatch-fase1` throughout, untouched).
- Built 4 binaries with `go build -o <scratchpad>\bins\noxy_<sha>.exe ./cmd/noxy`
  after `git checkout --detach <sha>` inside the worktree, for:
  - `f107508` — baseline (develop, pre-phase)
  - `bb8a773` — OP_CALL_STATIC (last commit before CallFrame value array)
  - `d460e5a` — CallFrame value array + its review fix-round
  - `6d991e5` — branch head (all 7 tasks)
- Verified all 4 binaries produce identical output
  (`CHECKSUM:3123750`) on `benchmarks\bench_share_mutate.nx` before timing.
- Ran three independent interleaved sessions (see raw data below), each
  randomizing binary order **within every round** via
  `Get-ChildItem`/`Sort-Object { $rand.Next() }`-style shuffling, so no
  binary is systematically favored by warm-up or thermal drift.
- Sessions 2 and 3 added a **same-binary duplicate control**: `f107508_A`
  and `f107508_B` both point at the identical `noxy_f107508.exe`, run under
  different labels in the same session. This isolates pure run-to-run/OS
  noise from any real binary-to-binary difference, inside the same session.
- Timed via `Measure-Command { & $exe $bench.nx | Out-Null }`. Session 3
  additionally batched 5 invocations per sample and divided by 5, to
  amortize process-spawn overhead — this did not materially change the
  picture (see below).
- Ran in the foreground, nothing else deliberately started. Background
  processes present throughout (typical dev-box set): WindowsTerminal,
  Slack, Cloudflare WARP, `logioptionsplus_agent`, `MsMpEng` (Defender),
  `CSFalconContainer` (CrowdStrike EDR), `gopls`. No benchmark or build was
  run concurrently by me. Session 3 shows clear late-round contamination
  (see below) most likely from one of these background agents (EDR/AV scan
  or similar) — flagged explicitly, not hidden.

## Raw measurements

### Session 1 — single invocation, N=15, no duplicate control

```
f107508: 189.79, 232.81, 209.49, 201.38, 231.48, 204.58, 227.21, 209.29, 194.81, 233.98, 254.61, 201.65, 231.27, 205.24, 206.67
bb8a773: 207.19, 205.08, 203.68, 213.22, 218.9, 204.38, 214.17, 191.99, 204.06, 197.08, 210.64, 203.25, 219.53, 200.67, 203.99
d460e5a: 267.65, 260.32, 236.69, 215.91, 218.31, 241.13, 242.29, 204.46, 224, 206.21, 260.47, 238.23, 210.92, 219.59, 234.79
6d991e5: 228.98, 222.74, 201.05, 207.56, 249.2, 245.77, 218.18, 231.48, 232.33, 270.26, 212.15, 230.15, 231.28, 245.66, 205.68
```

| label | n | median | mean | trimmed10 | stdev | CV |
|---|---|---|---|---|---|---|
| f107508 | 15 | 209.29 | 215.62 | 214.60 | 17.64 | 8.4% |
| bb8a773 | 15 | 204.38 | 206.52 | 206.64 | 7.37 | 3.6% |
| d460e5a | 15 | 234.79 | 232.06 | 231.45 | 19.45 | 8.3% |
| 6d991e5 | 15 | 230.15 | 228.83 | 227.78 | 18.11 | 7.9% |

### Session 2 — single invocation, N=20, with same-binary duplicate control

```
f107508_A: 190.44, 195.06, 211.61, 208.69, 202.92, 199.94, 198.43, 245.2, 203.11, 202.51, 197.2, 235.51, 278.29, 231.82, 201.02, 244.54, 198.2, 233.13, 195.54, 242.47
f107508_B: 189.45, 191.14, 192.18, 280.01, 190.51, 205.61, 209.56, 221.96, 212.13, 206.14, 188.14, 242.09, 201.17, 198.4, 210.3, 240.45, 205.3, 194.46, 219.57, 238.99
bb8a773:   203.72, 197.34, 201.21, 223.42, 184.21, 212.43, 201.87, 254.36, 199.24, 234.49, 220.44, 223.3, 271.76, 216.1, 204.11, 275.95, 208.49, 249.25, 224.41, 197.92
d460e5a:   192.86, 246.15, 212.78, 229.48, 208.05, 268.33, 212.47, 223.95, 203.82, 251.93, 204.92, 194.69, 214.96, 219.74, 212.55, 237.5, 243.95, 221.26, 205.93, 258.83
6d991e5:   220.97, 237.54, 201.52, 247.95, 212.61, 221.92, 250.74, 212.82, 214.63, 248.29, 213.3, 240.21, 226.94, 246.91, 217.45, 240.94, 219.78, 230.46, 230.48, 206.35
```

| label | n | median | mean | trimmed10 | stdev | CV |
|---|---|---|---|---|---|---|
| f107508_A | 20 | 203.02 | 215.78 | 212.91 | 23.17 | 11.4% |
| f107508_B | 20 | 205.88 | 211.88 | 208.62 | 22.73 | 11.0% |
| bb8a773 | 20 | 214.27 | 220.20 | 217.17 | 24.71 | 11.5% |
| d460e5a | 20 | 217.35 | 223.21 | 221.84 | 20.89 | 9.6% |
| 6d991e5 | 20 | 224.43 | 227.09 | 227.18 | 14.81 | 6.6% |

### Session 3 — batch-of-5 averaged, N=20, with duplicate control (late-round contamination)

```
f107508_A: 236.38, 203.42, 229.52, 214.7, 213.8, 221.77, 222.49, 209.32, 212, 231.94, 254.03, 236.95, 230.05, 223.99, 225.42, 242.77, 246.46, 220.67, 213.62, 233.18
f107508_B: 201.32, 205.87, 221.6, 223.18, 208.15, 221.42, 217.33, 203.28, 212.54, 222.84, 222.41, 214.92, 240.42, 222.73, 221.5, 249.9, 392.78, 297.86, 289.89, 228.33
bb8a773:   213.2, 256.47, 222.87, 218.02, 217.34, 211.36, 207.38, 242.21, 222.99, 218.76, 221.83, 215.2, 228.19, 231.9, 221.27, 228.62, 241.54, 248.49, 509.32, 222.18
d460e5a:   199.26, 234.01, 214.41, 215.45, 226.36, 239.47, 230.88, 228.8, 228.21, 252.37, 215.22, 257.54, 231.9, 248.08, 246.5, 217.93, 254.89, 216.43, 301.69, 247.33
6d991e5:   220.47, 228.06, 243.53, 236.59, 293.1, 230.33, 237.86, 246.47, 253.82, 232.65, 234.69, 218.89, 232.67, 237.73, 324.89, 271.8, 296.3, 305.03, 259.6, 232.67
```

Rounds 16-18 show clear spikes on several labels regardless of which commit
(e.g. `f107508_B`=392.78 at round 16, `bb8a773`=509.32 at round 18) —
consistent with an external agent (Defender/CrowdStrike scan or similar)
briefly stealing CPU, not a property of any binary. Reported both with and
without those samples.

| label | n | median | mean | trimmed10 | stdev | CV |
|---|---|---|---|---|---|---|
| f107508_A | 20 | 224.71 | 226.12 | 225.58 | 12.86 | 5.7% |
| f107508_B | 20 | 222.01 | 235.91 | 226.44 | 43.71 | 19.7% |
| bb8a773 | 20 | 222.53 | 239.96 | 225.91 | 63.03 | 28.3% |
| d460e5a | 20 | 231.39 | 235.34 | 233.36 | 21.71 | 9.4% |
| 6d991e5 | 20 | 237.80 | 251.86 | 247.99 | 29.60 | 12.4% |

Contamination-trimmed (drop the top 15% per label, N=17):

| label | median | mean | stdev |
|---|---|---|---|
| f107508_A | 222.49 | 222.31 | 9.67 |
| f107508_B | 221.50 | 219.87 | 12.07 |
| bb8a773 | 221.83 | 222.64 | 9.28 |
| d460e5a | 228.80 | 228.98 | 14.26 |
| 6d991e5 | 236.59 | 241.82 | 18.28 |

Trimming the contamination doesn't change the ranking or the rough
magnitudes — it mainly tightens the spread.

## Noise floor (same binary, two labels, same session)

| session | f107508_A median | f107508_B median | delta |
|---|---|---|---|
| s2 | 203.02 | 205.88 | +1.41% |
| s3 | 224.71 | 222.01 | -1.20% |

**With N=20 interleaved samples per label, the median-vs-median noise floor
for the literal same binary is ~1.2-1.4%.** Individual raw samples are far
noisier (stdev 6-12% of the median in clean stretches, up to ~20-28% CV
when a session catches background contamination) — this is why the medians
of 15-20 samples, not single runs, are the right comparison unit.

## Per-commit delta vs baseline (median-based)

| session | f107508 | bb8a773 | d460e5a | 6d991e5 (head) |
|---|---|---|---|---|
| s1 | 0.0% | -2.35% | +12.18% | +9.97% |
| s2 | 0.0%* | +4.80% | +6.31% | +9.78% |
| s3 | 0.0%* | -0.37% | +3.60% | +6.47% |

\* baseline = average of the two duplicate-label medians in that session.

**Baseline-to-HEAD delta is consistently positive across all three
independent sessions: +9.97%, +9.78%, +6.47% (mean +8.7%)** — well above
the ~1.4% same-binary noise floor. This corroborates the originally
reported +7.0% regression: it is real, not noise.

## Adjacent-step deltas — the actual bisection question

| step | s1 | s2 | s3 | mean |
|---|---|---|---|---|
| baseline -> bb8a773 | -2.35% | +5.54% | -0.97% | +0.74% |
| bb8a773 -> d460e5a (frame reuse) | **+14.88%** | +1.44% | +3.98% | +6.77% |
| d460e5a -> 6d991e5 (fused cmp/jump, INC_LOCAL, float ops) | -1.98% | +3.26% | +2.77% | +1.35% |

This is the crux and it does **not** point to a single clean culprit:

- **Session 1** shows a sharp, isolated cliff exactly at the frame-reuse
  step (bb8a773 -> d460e5a, +14.9%), with the steps before and after
  essentially flat/negative. Taken alone, this session is a clean smoking
  gun for the CallFrame-value-array hypothesis.
- **Sessions 2 and 3** instead show the regression building up gradually
  across all three steps, with no single step dominating (session 2:
  +5.5% / +1.4% / +3.3%; session 3: -1.0% / +4.0% / +2.8%). In session 2 the
  *first* step (baseline -> bb8a773, before frame reuse even lands) already
  accounts for more of the regression than the frame-reuse step itself.
- The **same step** (bb8a773 -> d460e5a) varies from +1.44% to +14.88%
  across sessions — a spread far larger than the ~1.4% same-binary noise
  floor measured in the same sessions. That means comparing *different*
  binaries carries session-level variability beyond pure run-to-run OS
  noise (plausibly: different code size/layout interacting differently
  with icache/TLB and whatever background load happens to be active during
  that binary's samples), which a same-binary duplicate label cannot
  capture since it has identical code layout.

Averaged across the three sessions, the frame-reuse step (bb8a773 ->
d460e5a, mean +6.8 points) is still the largest single contributor to the
~8.7% total, larger than either the OP_CALL_STATIC step before it (+0.7pp)
or the fused-opcode/float step after it (+1.4pp) — so the frame-reuse
hypothesis is the best-supported single explanation, but it is not proven
to the level of "regression appears cleanly and only at this commit" the
way session 1 alone would suggest. 2 of 3 sessions show meaningful
regression already present at or before `bb8a773`, and continuing to
accumulate after `d460e5a`.

## Conclusion

- **The end-to-end regression (baseline f107508 -> head 6d991e5) is real**:
  +9.97%, +9.78%, +6.47% across three independent interleaved sessions
  (mean +8.7%), consistent with the previously reported +7.0% and well
  above the measured ~1.4% same-binary noise floor.
- **Attribution to one single commit is not clean.** The CallFrame
  value-array commit and its fix round (`1ca26e7` / `d460e5a`) is the
  best-supported single contributor — it produced the single largest
  average step increase (+6.8 pp of +8.7 pp total) and, in one of three
  sessions, accounted for essentially the entire regression on its own.
  But in the other two sessions the regression was already partly present
  going into `bb8a773` and kept accumulating through `6d991e5`, so I
  cannot rule out a **diffuse, second-order GC/allocator-interaction
  effect that several of the phase's commits contribute to together**
  rather than one isolated cause. This is consistent with the profiling
  finding mentioned in the brief (no phase-1 code in the hot path — the
  cost is in allocation/GC pattern changes, which by nature can shift
  gradually as more of the phase lands, not just at the one commit that
  most obviously touches allocation).
- **Recommendation**: if a single culprit must be picked for the fix,
  `1ca26e7`/`d460e5a` (frame reuse) is the correct one to investigate
  first — it has the strongest single-session evidence and the largest
  average effect. But do not expect reverting it alone to fully close the
  gap to baseline, since roughly 2 of the 8.7 average points sit outside
  that step in 2 of 3 sessions.

## Commands used (representative)

```
git worktree add --detach "<scratchpad>\bisect" f107508
cd "<scratchpad>\bisect"
go build -o "<scratchpad>\bins\noxy_f107508.exe" ./cmd/noxy
git checkout --detach bb8a773
go build -o "<scratchpad>\bins\noxy_bb8a773.exe" ./cmd/noxy
git checkout --detach d460e5a
go build -o "<scratchpad>\bins\noxy_d460e5a.exe" ./cmd/noxy
git checkout --detach 6d991e5
go build -o "<scratchpad>\bins\noxy_6d991e5.exe" ./cmd/noxy

# correctness check
noxy_<sha>.exe benchmarks\bench_share_mutate.nx   # all -> CHECKSUM:3123750

# timing (see bisect_measure.ps1 / bisect_measure2.ps1 / bisect_measure3.ps1
# in the scratchpad for the exact interleaving/randomization/batching logic)
Measure-Command { & $exe $bench.nx | Out-Null }

git worktree remove --force "<scratchpad>\bisect"
```

## Cleanup confirmation

- `git worktree remove --force` succeeded for the bisect worktree.
- Main checkout (`D:\OneDrive\Documentos\go_projects\noxy`) `git status`
  after cleanup shows exactly the same pre-existing modified/untracked
  files that were present before this investigation started
  (`benchmarks/results/interleaved.md` modified; `bubble.prof`,
  `docs/superpowers/plans/2026-08-17-cow-rc-drops-fases-1_5-2.md`,
  `fib.prof`, `fib_pos.prof`, `loop.prof`, `share_mutate_pos.prof`
  untracked) — nothing from this investigation leaked into the main tree,
  and HEAD/branch (`perf/vm-dispatch-fase1`) was never touched.
- `git worktree list` after cleanup shows the main checkout, the unrelated
  `noxydb` worktree, and one other worktree
  (`.../scratchpad/base-review`, detached at `f107508`) that I did **not**
  create and did **not** remove — it lives under the same scratchpad
  session directory and was not present in `git worktree list` at the
  start of this task, so it belongs to a concurrent process/agent (the
  `.superpowers/sdd/2026-08-18-vm-perf-fase1-dispatch-e-chamadas/` folder
  already contains review diffs and task briefs/reports from that ongoing
  work). Left untouched as instructed.
