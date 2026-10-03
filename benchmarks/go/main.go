// Native Go references use int64, singly linked lists, structs, maps, and channels.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

func main() {
	workload := flag.String("workload", "", "workload name")
	samples := flag.Int("samples", 7, "measured runs")
	warmup := flag.Int("warmup", 2, "warmup runs")
	flag.Parse()
	if err := measure(*workload, *samples, *warmup); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func measure(name string, samples, warmup int) error {
	runs := map[string]func(){
		"arithmetic": arithmetic, "calls": calls, "structs": structs, "pointers": pointers,
		"lists": lists, "maps": maps, "strings": text, "messages": messages,
	}
	run, ok := runs[name]
	if !ok || samples < 1 || samples > 100 || warmup < 0 || warmup > 100 {
		return fmt.Errorf("invalid workload or sample count")
	}
	runtime.GOMAXPROCS(1)
	fmt.Println("Go:", runtime.Version())
	encoder := json.NewEncoder(os.Stdout)
	for round := 0; round < warmup+samples; round++ {
		// A fresh goroutine corresponds to each BEAM sample's fresh process.
		// Collection and memory-counter reads happen outside the timed interval.
		runtime.GC()
		type measurement struct {
			duration           time.Duration
			allocations, bytes uint64
		}
		finished := make(chan measurement)
		go func() {
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			start := time.Now()
			run()
			duration := time.Since(start)
			runtime.ReadMemStats(&after)
			finished <- measurement{duration, after.Mallocs - before.Mallocs, after.TotalAlloc - before.TotalAlloc}
		}()
		// Like the BEAM harness, fail a stuck sample without counting the
		// watchdog setup in execution time or workload allocation counts.
		timer := time.NewTimer(30 * time.Second)
		var result measurement
		select {
		case result = <-finished:
			timer.Stop()
		case <-timer.C:
			return fmt.Errorf("benchmark timeout: %s", name)
		}
		if round < warmup {
			continue
		}
		if err := encoder.Encode(struct {
			Workload       string `json:"workload"`
			Variant        string `json:"variant"`
			Microseconds   int64  `json:"microseconds"`
			Allocations    uint64 `json:"go_allocations"`
			AllocatedBytes uint64 `json:"go_allocated_bytes"`
		}{name, "go", result.duration.Microseconds(), result.allocations, result.bytes}); err != nil {
			return err
		}
	}
	return nil
}

func check(condition bool) {
	if !condition {
		panic("incorrect benchmark result")
	}
}

func arithmetic() {
	var total int64
	for i := int64(0); i < 100000; i++ {
		total += i
	}
	check(total == 4999950000)
}

func add(a, b int64) int64 { return a + b }

func calls() {
	var total int64
	for i := int64(0); i < 100000; i++ {
		total = add(total, i)
	}
	check(total == 4999950000)
}

type point struct{ x, y int64 }

func structs() {
	var p point
	for i := int64(0); i < 100000; i++ {
		p.x += i
		p.y++
	}
	check(p.x == 4999950000 && p.y == 100000)
}

type counter struct{ value int64 }

func increment(c *counter) { c.value++ }

func pointers() {
	var c counter
	for i := int64(0); i < 100000; i++ {
		increment(&c)
	}
	check(c.value == 100000)
}

type node struct {
	value int64
	next  *node
}

func lists() {
	var items *node
	for i := int64(0); i < 20000; i++ {
		items = &node{i, items}
	}
	var total, length int64
	for item := items; item != nil; item = item.next {
		total += item.value
	}
	for item := items; item != nil; item = item.next {
		length++
	}
	check(total == 199990000 && length == 20000)
}

func maps() {
	items := make(map[int64]int64)
	for i := int64(0); i < 10000; i++ {
		items[i] = i * 2
	}
	var total int64
	for i := int64(0); i < 10000; i++ {
		value, ok := items[i]
		check(ok)
		total += value
		if i%2 == 0 {
			delete(items, i)
		}
	}
	check(total == 99990000 && len(items) == 5000)
}

func text() {
	var total int64
	for i := 0; i < 10000; i++ {
		for _, field := range strings.Split(" 12, 34, 56, 78 ", ",") {
			value, err := strconv.ParseInt(strings.Trim(field, " \t\r\n"), 10, 64)
			check(err == nil)
			total += value
		}
	}
	check(total == 1800000)
}

func messages() {
	requests := make(chan int64)
	replies := make(chan int64)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 10000; i++ {
			replies <- <-requests
		}
	}()
	var total int64
	for i := int64(0); i < 10000; i++ {
		requests <- i
		total += <-replies
	}
	<-done
	check(total == 49995000)
}
