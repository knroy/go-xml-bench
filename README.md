# go-xml-bench

The benchmark harness for [go-xml](https://github.com/knroy/go-xml). It
compares go-xml with Saxon-HE, BaseX, xsltproc/xmllint (libxml2), Xerces-J
(XSD 1.1), Jing and Go's `encoding/xml` on the workloads in `workloads/`.
Every item is checked for agreement with go-xml before it is timed; an engine
that disagrees on an item is recorded with `"agree": false` and the reason,
and gets no timing for that item.

go-xml's [docs/benchmark.md](https://github.com/knroy/go-xml/blob/main/docs/benchmark.md)
reports the results, and its
[docs/profiling.md](https://github.com/knroy/go-xml/blob/main/docs/profiling.md)
shows how to profile with this harness.

## Setup

This repository lives at `bench/` inside a go-xml checkout. The harness reads
its inputs from the checkout's `testdata/`, builds the go-xml command-line
tool from it, and measures the checked-out code: `go.mod` replaces
`github.com/knroy/go-xml/v2` with `../`. go-xml ignores `bench/`.

    git clone https://github.com/knroy/go-xml
    git clone https://github.com/knroy/go-xml-bench go-xml/bench
    cd go-xml/bench

The harness follows go-xml's v2 API, on go-xml's `main` branch.

### Prerequisites

- Go (the version in `go.mod`).
- Java 17+ with `javac` on `PATH`, for the JVM engines and `java/Loop.java`.
- `xsltproc` and `xmllint` on `PATH` (system libxml2; not fetched). An engine
  that is missing is reported as skipped, not as a failure.
- `curl`, `unzip`, `tar`, a C compiler (`cc`, for XMark's `xmlgen`), and
  `sha256sum` or `shasum`, for the fetch scripts.

### Fetch the engines and inputs

    sh fetch-engines.sh
    sh fetch-inputs.sh

`fetch-engines.sh` downloads the pinned jars (versions, URLs and SHA-256 in
`engines.json`) into `.cache/`. `fetch-inputs.sh` downloads, verifies and
generates the inputs into the checkout's `testdata/bench/`, and writes their
sources and licences to `testdata/bench/SOURCES.md`. Both are idempotent; a
file whose digest does not match is refused, never overwritten.

## Run

From `bench/`:

    go run ./cmd/benchrun                             # every workload -> results.json
    go run ./cmd/benchrun -workload selftest -out .cache/selftest.json
    go run ./cmd/benchrun -workload X -engine saxon-he -mode warm
    go run ./cmd/benchrun -workload X -check-only

Flags: `-workload` and `-engine` (repeatable), `-mode cold|warm|both`,
`-runs` / `-warmup` (cold: timed and untimed processes, default 10 and 2),
`-warm-iters` (warm: timed passes per item, default 30, after a fifth as many
untimed), `-out`, `-check-only`. `selftest` runs the tiny fixtures in
`testfixtures/` through every engine and mode, to check the harness itself.

## Results

Each release's run is kept here; a new run writes `results.json`, which is
not tracked.

| File | go-xml | Machine |
|---|---|---|
| `results-v1-f45068c.json` | v1 at `f45068c` | Apple M3 Pro, 12 cores, Go 1.26.4, OpenJDK 23.0.2 |
| `results-v2.0.0.json` | v2.0.0 | Apple M3 Pro, 12 cores, Go 1.26.4, OpenJDK 23.0.2 |

## The two modes

- **cold** — one OS process per run: start, compile, run, write the output to
  a file. Wall time per run and peak RSS (`getrusage` max RSS, normalised to
  bytes; not measured on Windows).
- **warm** — steady state: compile once, then time each item in-process. For
  go-xml and `encoding/xml` this is a Go loop in benchrun over the public APIs;
  for the JVM engines it is `java/Loop.java` through the engine's Java API.
  Inputs are read into memory first and output goes to a null sink, so every
  engine's warm time per item is parse the input, evaluate, serialise. libxml2
  tools have no in-process harness, so they are cold only. BaseX binds the
  input when it compiles a query, so it compiles per item: its warm time
  includes that compile (under 1 ms on XMark, against 10–110 ms to parse the
  input), and it reports no compile time.

The parse and c14n areas have no go-xml CLI command; their cold process is
`benchrun -helper ENGINE SOURCE OUT`, which parses and writes Canonical XML.

## Workload files

`workloads/<id>.json`: `id`, `area` (`xslt`, `xquery`, `xsd`, `rng`, `parse`,
`c14n`), `description`, `inputs`, `items`, `normalise` (`none`, `whitespace`,
`text`, `xml-c14n`), `engines` (must include `go-xml`), `modes`. Each item has
`name`, `stylesheet`/`query`/`schema`, `source` and optional `params`; `glob`
in place of `source` expands to one item per match; `area` on an item
overrides the workload's. Optional: `xsd_version` (go-xml's, default `1.1`)
and `args` (`{"engine": ["extra", "argv"]}`; for go-xml's warm loop only
`-allow-dir`, `-allow-doctype`, `-allow-unparsed-text` and
`-compat-drop-attributes-on-document` are understood), and `strip`, regular
expressions whose matches are deleted from every engine's output before the
normalise rule — for output that differs by design (a timestamp, the
processor's name), never for a real difference. Paths are relative to the
go-xml checkout's root. A workload that does not load (say, a glob matching nothing)
stops only a run that asks for it.

Validators agree on the verdict (valid, invalid, error). Other areas agree on
the normalised output, or on both engines failing — with the same error code
when both name one.

## XSLT workloads

- `docbook-xsltng` — DocBook xslTNG over 42 listed unit-test documents.
- `xrechnung-xr`, `xrechnung-html` — the KoSIT XRechnung chain, one workload
  per stage, since benchrun does not chain. Run `sh xrechnung-stage1.sh`
  first: it writes stage 1's go-xml output to `.cache/xrechnung-xr/`, the
  sources of `xrechnung-html`.
- `peppol-schematron` — the SchXslt-compiled CEN and Peppol rule sets → SVRL.

All four are XSLT 2.0 or 3.0, so xsltproc is not among their engines. Each
file's `description` gives its exclusions and the reason for its `normalise`
and `strip`.

## Licence

MIT; see [LICENSE](LICENSE). The reference engines and the benchmark inputs
are not part of this repository. The fetch scripts download them from their
publishers, each under its own licence: `engines.json` lists the engines, and
`fetch-inputs.sh` records the inputs' sources and licences.
