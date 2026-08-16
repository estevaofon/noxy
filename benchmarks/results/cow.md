# Benchmark results: cow

- Binary: `.\noxy-cow.exe`
- Date: 2026-08-16T16:21:37
- Runs per bench: 5 (median reported)

| bench | median_ms | runs_ms | checksum |
|---|---|---|---|
| bench_bubblesort.nx | 4554.4 | 4608,6 4457,3 4555,5 4554,4 4376,9 | CHECKSUM:942765099 |
| bench_call_light.nx | 2817.6 | 2777,3 2738,3 2886,6 2825,2 2817,6 | CHECKSUM:103980 |
| bench_call_readonly.nx | 1967.8 | 1916 2124,1 2171,4 1967,8 1906,8 | CHECKSUM:59997000000 |
| bench_call_ref.nx | 4591.5 | 5144,6 4593 4591,5 4385,7 4523,4 | CHECKSUM:1096990000 |
| bench_conway.nx | 2753.8 | 2637,8 2642,7 2795,8 2753,8 2761,7 | CHECKSUM:21042 |
| bench_map_churn.nx | 447.9 | 427,4 447,9 485,2 480,1 447,2 | CHECKSUM:1232790 |
| bench_path_update.nx | 645.4 | 645,4 654,9 636,3 648 603,8 | CHECKSUM:250500000 |
| bench_share_mutate.nx | 650.6 | 612,4 618,3 673,3 650,6 664,3 | CHECKSUM:3123750 |
| bench_spawn_sum.nx | 1008.7 | 1004,4 1077,4 1091 1008,7 975 | CHECKSUM:8000004000360 |
