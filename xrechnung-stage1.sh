#!/bin/sh
# Pre-step for the xrechnung-html workload: runs KoSIT stage 1
# (ubl-invoice-xr.xsl, UBL Invoice -> xr:invoice) over every
# testdata/bench/xrechnung/invoices/*.xml with go-xml, writing
# bench/.cache/xrechnung-xr/<name>.xml as the stage-2 sources. benchrun has
# no chaining; the xrechnung-xr workload checks this same stage against
# Saxon. Run after bench/fetch-inputs.sh, from any directory.
set -eu
cd "$(dirname "$0")/.."
X=testdata/bench/xrechnung
OUT=bench/.cache/xrechnung-xr
[ -f "$X/xsl/ubl-invoice-xr.xsl" ] || { echo "$X not fetched: run bench/fetch-inputs.sh" >&2; exit 1; }
mkdir -p "$OUT"
BIN="$OUT/go-xml"
go build -o "$BIN" ./cmd/go-xml
for f in "$X"/invoices/*.xml; do
	"$BIN" -xsl "$X/xsl/ubl-invoice-xr.xsl" -o "$OUT/$(basename "$f")" "$f"
done
rm -f "$BIN"
echo "wrote $OUT"
