package compiler

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrimePoolAssignmentProtocol(t *testing.T) {
	var sources []SourceFile
	for _, name := range []string{"messages.lang", "workers.lang", "queue.lang"} {
		filename := filepath.Join("../../examples/prime_lab", name)
		source, err := os.ReadFile(filename)
		if err != nil {
			t.Fatal(err)
		}
		sources = append(sources, SourceFile{Filename: filename, Source: source})
	}
	// Controlled workers expose assignments without doing work. Replies all
	// originate from this test driver, giving the queue a deterministic order
	// for completion, duplicate delivery, replacement, and stale-PID messages.
	testMain := `
type TestArgs struct { queue Pid; parent Pid; slot int }
type Receipt struct { slot int; pid Pid; work Work }
func controlled(args TestArgs) {
 send(args.queue, Command{kind: WorkerReady, slot: args.slot, worker: self()})
 for {
  work := receive[Work](-1).value
  if work.job.id == 0 { return }
  send(args.parent, Receipt{slot: args.slot, pid: self(), work: work})
 }
}
func receipt() Receipt {
 delivery := receive[Receipt](5000)
 if !delivery.ok { panic("assignment timed out") }
 return delivery.value
}
func done(queue Pid, assignment Receipt, count int) {
 send(queue, Command{kind: JobFinished, slot: assignment.slot, worker: assignment.pid,
  result: Result{id: assignment.work.job.id, limit: assignment.work.job.limit, count: count}})
}
func main() {
 queue := spawnMonitor(queue, QueueArgs{parent: self(), workers: 3})
 var children List[Process]
 for slot := 1; slot <= 3; slot++ {
  children = append(children, spawnMonitor(controlled, TestArgs{queue: queue.pid, parent: self(), slot: slot}))
 }
 send(queue.pid, Command{kind: WorkerReady, slot: 0, worker: self()})
 send(queue.pid, Command{kind: WorkerReady, slot: 4, worker: self()})
 send(queue.pid, Command{kind: WorkerReady, slot: 1})
 jobs := List[Job]{Job{id: 1, limit: 11}, Job{id: 2, limit: 12}, Job{id: 3, limit: 13}, Job{id: 4, limit: 14}, Job{id: 5, limit: 15}}
 send(queue.pid, Command{kind: Enqueue, jobs: jobs})
 var initial Map[int,Receipt]
 for i := 0; i < 3; i++ {
  assignment := receipt()
  if assignment.work.job.id != assignment.slot || assignment.work.crash { panic("initial assignment mismatch") }
  initial = put(initial, assignment.slot, assignment)
 }
 first := get(initial, 1).value
 second := get(initial, 2).value
 third := get(initial, 3).value
 // Complete out of order and reuse the second worker for job 4.
 done(queue.pid, second, 2)
 fourth := receipt()
 if fourth.slot != 2 || fourth.work.job.id != 4 { panic("worker was not reused") }
 // A duplicate job 2 reply must not free job 4. Nor may another PID claim it.
 done(queue.pid, second, 999)
 send(queue.pid, Command{kind: WorkerReady, slot: fourth.slot, worker: fourth.pid})
 impostor := fourth
 impostor.pid = first.pid
 done(queue.pid, impostor, 999)
 done(queue.pid, third, 3)
 fifth := receipt()
 if fifth.slot != 3 || fifth.work.job.id != 5 { panic("duplicate or wrong PID freed an assignment") }
 replacement := spawnMonitor(controlled, TestArgs{queue: queue.pid, parent: self(), slot: 1})
 children = append(children, replacement)
 retried := receipt()
 if retried.slot != 1 || retried.work.job.id != 1 || retried.pid == first.pid { panic("unfinished job was not retried") }
 // Old PID acknowledges the retried job before its replacement does.
 done(queue.pid, first, 999)
 done(queue.pid, fourth, 4)
 done(queue.pid, fourth, 999)
 done(queue.pid, fifth, 5)
 done(queue.pid, retried, 1)
 delivery := receive[Summary](5000)
 if !delivery.ok { panic("pool did not finish") }
 summary := delivery.value
 if len(summary.results) != 5 || summary.restarts != 1 || summary.peak != 3 { panic("pool totals mismatch") }
 for i, result := range summary.results {
  if result.id != i + 1 || result.count != i + 1 || result.limit != i + 11 { panic("stale result or unordered output") }
 }
 exited := wait(queue.monitor, 5000)
 if !exited.ok || !exited.normal { panic("queue leaked or crashed") }
 for _, child := range children { send(child.pid, Work{}) }
 for _, child := range children {
  exit := wait(child.monitor, 5000)
  if !exit.ok || !exit.normal { panic("test worker leaked") }
 }
 println("ordered results, retries, duplicate and stale replies: ok")
}`
	sources = append(sources, SourceFile{Filename: "protocol_test.lang", Source: []byte("package main\n" + testMain)})
	script := `logger:set_primary_config(level, emergency), linglang_rt:set_gc_stress(true),
 linglang_program:main(), undefined = get(linglang_supervisors),
 #{live_cells := 0, root_frames := 0} = linglang_rt:stats(), halt(0).`
	want := "ordered results, retries, duplicate and stale replies: ok\n"
	for _, options := range []Options{{}, {DisableOptimizations: true}} {
		program, err := CompileFilesWithOptions(sources, options)
		if err != nil {
			t.Fatal(err)
		}
		got, err := executeCompiledScript(t, program, script)
		if err != nil || got != want {
			t.Fatalf("options=%+v: got %q (%v), want %q", options, got, err, want)
		}
	}
}
