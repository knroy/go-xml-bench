package main

// TestProfile profiles one workload's warm loop (compile once, then parse →
// run → serialise to io.Discard, exactly as warmGo does). It is skipped unless
// PROF_WORKLOAD is set:
//
//	PROF_WORKLOAD=docbook-xsltng PROF_OUT=/tmp/p PROF_PASSES=10 \
//	  go test ./cmd/benchrun -run TestProfile -count=1 -v
//
// PROF_ITEMS (a substring) keeps matching items; PROF_TYPED=1 runs xsd items
// through ValidateCopy instead of Validate. Writes cpu.pprof, allocs.base and
// allocs.pprof (diff with -base) to PROF_OUT and logs getrusage CPU,
// MemStats deltas and the runtime's GC CPU estimate for the timed passes.

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/metrics"
	"runtime/pprof"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/knroy/go-xml/v2/xdm"
	"github.com/knroy/go-xml/v2/xsd"
)

func TestProfile(t *testing.T) {
	id := os.Getenv("PROF_WORKLOAD")
	if id == "" {
		t.Skip("PROF_WORKLOAD not set")
	}
	// PROF_MODE=mem samples allocations (allocs.base, allocs.pprof) and takes
	// no CPU profile; otherwise sampling is off, as in a binary that does not
	// link runtime/pprof (the sampler costs up to 15% of XSLT CPU).
	mem := os.Getenv("PROF_MODE") == "mem"
	if !mem {
		runtime.MemProfileRate = 0
	}
	out := os.Getenv("PROF_OUT")
	passes, _ := strconv.Atoi(os.Getenv("PROF_PASSES"))
	if passes == 0 {
		passes = 10
	}
	root, _ := filepath.Abs("../../..")
	ws, err := loadWorkloads(filepath.Join(root, "bench/workloads"), root, []string{id})
	if err != nil {
		t.Fatal(err)
	}
	w := ws[0]
	filter := os.Getenv("PROF_ITEMS")
	typed := os.Getenv("PROF_TYPED") == "1"

	// One run func per compile-once group, as runWorkload groups them.
	type job struct {
		run runFunc
		src []byte
		uri string
		it  Item
	}
	var jobs []job
	runs := map[string]runFunc{}
	for _, it := range w.Items {
		if filter != "" && !strings.Contains(it.Name, filter) {
			continue
		}
		in := it.compileInput()
		key := it.Area + "\x00" + in
		run := runs[key]
		if run == nil {
			abs := ""
			if in != "" {
				abs = filepath.Join(root, in)
			}
			if typed && it.Area == "xsd" {
				run, err = compileTyped(abs, w.XSDVersion)
			} else {
				run, err = compileGo("go-xml", it.Area, abs, w.Args["go-xml"], w.XSDVersion)
			}
			if err != nil {
				t.Fatal(err)
			}
			runs[key] = run
		}
		src, err := os.ReadFile(filepath.Join(root, it.Source))
		if err != nil {
			t.Fatal(err)
		}
		jobs = append(jobs, job{run, src, fileURI(filepath.Join(root, it.Source)), it})
	}
	pass := func() int {
		errs := 0
		for _, j := range jobs {
			if j.run(j.src, j.uri, j.it.Params) != nil {
				errs++
			}
		}
		return errs
	}
	for i := 0; i < max(passes/5, 1); i++ {
		pass()
	}

	write := func(name string) {
		if !mem {
			return
		}
		f, err := os.Create(filepath.Join(out, name))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := pprof.Lookup("allocs").WriteTo(f, 0); err != nil {
			t.Fatal(err)
		}
	}
	runtime.GC()
	write("allocs.base")
	cpuPath := filepath.Join(out, "cpu.pprof")
	if mem {
		cpuPath = os.DevNull
	}
	cpu, err := os.Create(cpuPath)
	if err != nil {
		t.Fatal(err)
	}
	samples := []metrics.Sample{{Name: "/cpu/classes/gc/total:cpu-seconds"}, {Name: "/cpu/classes/total:cpu-seconds"},
		{Name: "/cpu/classes/idle:cpu-seconds"}, {Name: "/cpu/classes/gc/mark/idle:cpu-seconds"},
		{Name: "/cpu/classes/gc/mark/assist:cpu-seconds"}}
	var m0, m1 runtime.MemStats
	runtime.ReadMemStats(&m0)
	metrics.Read(samples)
	gc0, tot0, idle0 := samples[0].Value.Float64(), samples[1].Value.Float64(), samples[2].Value.Float64()
	gcIdle0, assist0 := samples[3].Value.Float64(), samples[4].Value.Float64()
	r0 := rusage()
	start := time.Now()
	if !mem {
		pprof.StartCPUProfile(cpu)
	}
	errs := 0
	for i := 0; i < passes; i++ {
		errs += pass()
	}
	if !mem {
		pprof.StopCPUProfile()
	}
	wall := time.Since(start)
	r1 := rusage()
	metrics.Read(samples)
	runtime.ReadMemStats(&m1)
	cpu.Close()
	write("allocs.pprof")
	gc := samples[0].Value.Float64() - gc0
	busy := (samples[1].Value.Float64() - tot0) - (samples[2].Value.Float64() - idle0)
	gcIdle, assist := samples[3].Value.Float64()-gcIdle0, samples[4].Value.Float64()-assist0
	n := float64(passes)
	report := fmt.Sprintf("mem=%v workload=%s typed=%v items=%d passes=%d errs/pass=%d\n"+
		"wall/pass=%.2fms rusageCPU/pass=%.2fms\n"+
		"allocs/pass=%.0f bytes/pass=%.2fMB gc-cycles/pass=%.2f\n"+
		"runtime GC CPU share=%.1f%% (gc %.3fs of %.3fs non-idle); of which idle-P mark %.1f%%, assists %.1f%%; GC share without idle-P mark %.1f%%\n",
		mem, id, typed, len(jobs), passes, errs/passes,
		float64(wall.Microseconds())/1e3/n, (r1-r0).Seconds()*1e3/n,
		float64(m1.Mallocs-m0.Mallocs)/n, float64(m1.TotalAlloc-m0.TotalAlloc)/1e6/n,
		float64(m1.NumGC-m0.NumGC)/n, 100*gc/busy, gc, busy, 100*gcIdle/busy, 100*assist/busy,
		100*(gc-gcIdle)/(busy-gcIdle))
	os.WriteFile(filepath.Join(out, map[bool]string{false: "summary-cpu.txt", true: "summary-mem.txt"}[mem]), []byte(report), 0o644)
	t.Log("\n" + report)
}

func rusage() time.Duration {
	var ru syscall.Rusage
	syscall.Getrusage(syscall.RUSAGE_SELF, &ru)
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
}

// compileTyped is compileGo's xsd loop with ValidateCopy (typed validation).
func compileTyped(input, xsdVersion string) (runFunc, error) {
	v := xsd.Version11
	if xsdVersion == "1.0" {
		v = xsd.Version10
	}
	schema, err := xsd.LoadFiles([]string{input}, xsd.Options{Version: v})
	if err != nil {
		return nil, err
	}
	return func(src []byte, uri string, _ map[string]string) error {
		tree, err := xdm.ParseString(string(src), xdm.ParseOptions{BaseURI: uri, DocumentURI: uri,
			MaxBytes: -1, MaxNodes: -1, TrackPositions: true})
		if err != nil {
			return err
		}
		schema.ValidateCopy(tree.Root, xsd.ValidateOptions{})
		return nil
	}, nil
}
