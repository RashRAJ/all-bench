# all-bench

A unified CLI wrapper for LLM benchmarking tools. Configure once in YAML, run any supported tool with a single command, and get normalized output.

## Supported runners

| Runner | Tool | Install |
|--------|------|---------|
| `aiperf` | [NVIDIA AIPerf](https://github.com/ai-dynamo/aiperf) — throughput & latency for text LLMs | `pip install aiperf` |
| `vlmbench` | [vlmbench](https://github.com/vlm-run/vlmbench) — throughput & latency for VLMs and text LLMs | `pip install vlmbench` |

## Requirements

- Go 1.21+
- Python 3.11 or 3.12 (3.14 is not supported by aiperf/vlmbench)
- A running inference server (vLLM, Ollama, etc.)

## Install

```bash
git clone https://github.com/rashee/all-bench
cd all-bench
go build -o all-bench .
```

## Quick start

```bash
# check which runners are installed
./all-bench list

# run with defaults from all-bench.yaml
./all-bench run

# run a specific runner
./all-bench run --runner aiperf

# override output format
./all-bench run --format json

# write results to a file
./all-bench run --out results.json
```

## Configuration

All settings live in `all-bench.yaml`. Shared values go under `defaults`; runner-specific config goes in the runner's own section.

```yaml
defaults:
  model: mistralai/Mistral-7B-Instruct-v0.3
  output_tokens: 128

runners:
  - aiperf
  # - vlmbench

aiperf:
  url: localhost:8000
  endpoint_type: chat
  endpoint: /v1/chat/completions
  streaming: true
  request_rate: 32        # req/s, Poisson distribution
  request_count: 64       # total requests to send

vlmbench:
  url: http://localhost:8001/v1
  dataset: hf://vlm-run/FineVision-vlmbench-mini
  dataset_text_col: prompt
  prompt: "Explain GPU observability in 3 sentences."
  concurrency: "4,8,16"  # sweep — produces one result per level
  max_samples: 64

output:
  format: table           # table | json
  file: results.json      # optional — omit to skip file write
```

### aiperf options

| Field | Description |
|-------|-------------|
| `url` | Inference server address (e.g. `localhost:8000`) |
| `endpoint_type` | `chat` \| `completions` \| `embeddings` |
| `endpoint` | API path (e.g. `/v1/chat/completions`) |
| `streaming` | Enable streaming responses |
| `request_rate` | Requests/sec using Poisson distribution |
| `concurrency` | Fixed concurrent workers (takes priority over `request_rate`) |
| `request_count` | Total number of requests to send |

### vlmbench options

| Field | Description |
|-------|-------------|
| `url` | Inference server base URL |
| `dataset` | HuggingFace dataset (e.g. `hf://vlm-run/FineVision-vlmbench-mini`) |
| `dataset_text_col` | Column to use as prompt for text-only benchmarks |
| `dataset_split` | Dataset split to load (default: `train`) |
| `input` | Local file or directory (images, PDFs, video) |
| `prompt` | Instruction sent with each input. Set `""` to use the text column as the full message |
| `backend` | `auto` \| `ollama` \| `vllm` \| `vllm-openai:latest` |
| `concurrency` | Single value or comma-separated sweep: `"4,8,16"` |
| `max_samples` | Limit number of inputs (useful for quick checks) |
| `runs` | Timed runs per input (default: 3) |

## Sample runs

### aiperf — text LLM throughput & latency

```bash
$ ./all-bench run --runner aiperf
Running aiperf...
INFO     Starting AIPerf System
INFO     AIPerf System is PROFILING

Profiling: 64/64 |████████████████████████| 100% [00:44<00:00]

INFO     Benchmark completed successfully

            NVIDIA AIPerf | LLM Metrics
┏━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┳━━━━━━━━━━━┳━━━━━━━━━━┳━━━━━━━━━━━┳━━━━━━━━━━━┳━━━━━━━━━━━┳━━━━━━━━━━━┳━━━━━━━━━━┓
┃                                    Metric ┃       avg ┃      min ┃       max ┃       p99 ┃       p90 ┃       p50 ┃      std ┃
┡━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━╇━━━━━━━━━━━╇━━━━━━━━━━╇━━━━━━━━━━━╇━━━━━━━━━━━╇━━━━━━━━━━━╇━━━━━━━━━━━╇━━━━━━━━━━┩
│              Time to First Token (ms)     │ 10,077.02 │  6,360.55│ 14,009.07 │ 13,978.01 │ 13,698.47 │  9,936.90 │ 2,782.20 │
│            Time to Second Token (ms)      │    385.59 │     53.22│    744.90 │    731.66 │    612.48 │    459.20 │   225.59 │
│          Time to First Output Token (ms)  │ 10,077.02 │  6,360.55│ 14,009.07 │ 13,978.01 │ 13,698.47 │  9,936.90 │ 2,782.20 │
│                  Request Latency (ms)     │ 35,014.65 │ 24,969.85│ 43,685.93 │ 43,655.47 │ 43,381.31 │ 36,650.22 │ 6,777.50 │
│              Inter Token Latency (ms)     │    147.09 │     98.75│    174.44 │    173.92 │    169.28 │    148.68 │    22.56 │
│  Output Token Throughput Per User (t/s/u) │      7.00 │      5.73│     10.13 │      9.95 │      8.36 │      6.73 │     1.35 │
│        Output Sequence Length (tokens)    │    166.86 │    112.00│    188.00 │    187.64 │    184.40 │    180.00 │    26.14 │
│         Input Sequence Length (tokens)    │    550.00 │    550.00│    550.00 │    550.00 │    550.00 │    550.00 │     0.00 │
│    Output Token Throughput (tokens/sec)   │     26.40 │       N/A│       N/A │       N/A │       N/A │       N/A │      N/A │
│       Request Throughput (requests/sec)   │      0.16 │       N/A│       N/A │       N/A │       N/A │       N/A │      N/A │
│              Request Count (requests)     │      7.00 │       N/A│       N/A │       N/A │       N/A │       N/A │      N/A │
└───────────────────────────────────────────┴───────────┴──────────┴───────────┴───────────┴───────────┴───────────┴──────────┘

CLI Command: aiperf profile --model 'mistralai/Mistral-7B-Instruct-v0.3' --url 'localhost:8000' \
             --endpoint-type 'chat' --endpoint '/v1/chat/completions' \
             --request-count 64 --streaming --request-rate 32
Benchmark Duration: 44.24 sec
JSON Export: artifacts/mistralai_Mistral-7B-Instruct-v0.3-openai-chat-request_rate32.0/profile_export_aiperf.json
```

### vlmbench — VLM & text LLM benchmarking with concurrency sweep

```bash
$ ./all-bench run --runner vlmbench
Running vlmbench...

╭─ Configuration ──────────────────────────────────────────────────────────────╮
│                                                                              │
│  model        Qwen/Qwen3-VL-2B-Instruct                                      │
│  backend      vLLM 0.11.2                                                    │
│  endpoint     http://localhost:8001/v1                                       │
│                                                                              │
│  dataset      hf://vlm-run/FineVision-vlmbench-mini                          │
│  samples      64                                                             │
│  prompt       Explain GPU observability in 3 sentences.                      │
│                                                                              │
│  max_tokens   128                                                            │
│  runs         3                                                              │
│  concurrency  4,8,16                                                         │
│                                                                              │
╰──────────────────────────────────────────────────────────────────────────────╯

# concurrency=4
╭─ Results ────────────────────────────────────────────────────────────────────╮
│  Metric                Value           p50        p95        p99             │
│  Throughput            4.21 img/s       —          —          —              │
│  Tokens/sec            412 tok/s        —          —          —              │
│  Workers               4               —          —          —              │
│  TTFT                  72 ms        65 ms     138 ms     159 ms              │
│  TPOT                  8.1 ms       7.6 ms    11.2 ms    12.0 ms             │
│  Latency (per worker)  0.95 s/img   0.88 s     1.42 s     1.61 s            │
╰──────────────────────────────────────────────────────────────────────────────╯

# concurrency=8
╭─ Results ────────────────────────────────────────────────────────────────────╮
│  Metric                Value           p50        p95        p99             │
│  Throughput            7.89 img/s       —          —          —              │
│  Tokens/sec            821 tok/s        —          —          —              │
│  Workers               8               —          —          —              │
│  TTFT                  58 ms        51 ms     114 ms     140 ms              │
│  TPOT                  5.3 ms       5.0 ms     7.3 ms     7.4 ms             │
│  Latency (per worker)  0.54 s/img   0.46 s     0.92 s     1.36 s            │
╰──────────────────────────────────────────────────────────────────────────────╯

# concurrency=16
╭─ Results ────────────────────────────────────────────────────────────────────╮
│  Metric                Value           p50        p95        p99             │
│  Throughput            13.33 img/s      —          —          —              │
│  Tokens/sec            1168 tok/s       —          —          —              │
│  Workers               16              —          —          —              │
│  TTFT                  91 ms        84 ms     201 ms     238 ms              │
│  TPOT                  9.7 ms       9.1 ms    14.8 ms    16.2 ms             │
│  Latency (per worker)  0.61 s/img   0.54 s     1.18 s     1.52 s            │
╰──────────────────────────────────────────────────────────────────────────────╯

Results written to results.json
```

> Both runners print their own native output directly. `--format json` or `--out <file>` writes normalized results for scripting or cross-runner comparison.

## Adding a runner

1. Implement the `Runner` interface in `runner/yourrunner.go`:
```go
type Runner interface {
    Name()            string
    Available()       bool
    InstallHint()     string
    HasNativeOutput() bool   // true = skip all-bench's table, use the tool's own output
    Run(cfg *config.Config) ([]*Result, error)
}
```
2. Add a section to `config.Config` for runner-specific fields
3. Register it in `cmd/run.go` and `cmd/list.go`

## Project structure

```
all-bench/
├── main.go
├── all-bench.yaml
├── cmd/
│   ├── root.go
│   ├── run.go        # config → runner → output
│   └── list.go       # show runner install status
├── config/
│   └── config.go     # YAML schema
├── runner/
│   ├── runner.go     # Runner interface + Result types
│   ├── aiperf.go
│   └── vlmbench.go
└── output/
    └── output.go     # table + JSON rendering
```
