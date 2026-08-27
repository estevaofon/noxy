# Noxy Concurrency Guide

Noxy provides first-class support for concurrency through **Noxy Routines** and **Channels**. Inspired by Go's goroutines and CSP (Communicating Sequential Processes) model, Noxy allows you to write concurrent programs that are simple, safe, and highly performant.

## core Concepts

1.  **Noxy Routines**: Lightweight threads managed by the Noxy Runtime (VM). They are cheap to create (much cheaper than OS threads) and are multiplexed onto OS threads.
2.  **Channels**: Typed conduits that allow Noxy routines to communicate with each other and synchronize their execution.
3.  **Shared State**: While channels are preferred, Noxy routines share the same global variables and module state, allowing for shared-memory patterns (though message passing is recommended).

### Runtime sharing guarantees

Routines whose VMs share one runtime state see the same global bindings, module cache, and open file, network, SQLite database, and SQLite statement handles. A stateful builtin always uses the VM that is actively executing the call, so a handle created in one shared VM can be used from another.

Individual global-binding and map operations are synchronized and do not crash the Go runtime when used concurrently. Concurrent module requests share one successful initialization, and individual resource registries and resource state are also synchronized.

These guarantees do not make a sequence of operations atomic. For example, `counter = counter + 1` is still a separate read and write. Likewise, arrays, structs, and composite values nested inside a map or global binding are not recursively synchronized. Coordinate compound operations and mutation of nested composite values with channels (or another explicit single-owner protocol).

Normal function calls, `spawn`, and `spawn_task` all follow Noxy's value semantics (0.4.0+): an ordinary composite parameter is an independent value at any depth, implemented with copy-on-write — the legacy `spawn` identity exception was removed. Data handed to another routine by argument or channel is therefore race-free by construction — unless its type is *ref-carrying* (a field, element or map value declared `ref T`; spec §2.2 rule 6): a `ref` field travels with the copy as a shared edge, exactly like a `ref` argument, and the sharing is declared in the struct, not at the `spawn`. Intentionally shared state (globals, `ref` — as argument or as field) still requires explicit coordination; the synchronized runtime foundation does not make compound mutations atomic.

A closure that captures a `ref` to a local shares that local's cell with
every routine that runs the closure — it is a local only in name:

```noxy
func main()
    let counter: int = 0
    let rc: ref int = ref counter       // counter is now a heap cell
    let bump: func() -> void = func() -> void
        *rc = *rc + 1                   // shared with main through rc
    end
    spawn(bump)
    spawn(bump)
    // *rc is read and written by three routines: coordinate through a
    // channel, or hand each routine its own value instead.
end
```

This foundation does not change the behavior of existing concurrency primitives. It also underpins supervised tasks, described below, without changing detached `spawn`.

---

## Spawning Routines

To start a new routine, use the `spawn` built-in function. It takes a function and its arguments.

```noxy
func worker(id: int)
    print(fmt("Worker %d is running", id))
end

func main()
    // Spawns 3 routines that run concurrently with main
    spawn(worker, 1)
    spawn(worker, 2)
    spawn(worker, 3)
    
    // Main needs to wait (e.g., using sleep or channels), 
    // otherwise the program exits when main finishes.
    sleep(100) 
end
```

### Key Characteristics
- **Non-blocking**: `spawn` returns immediately.
- **Concurrent**: The spawned function runs in parallel (on multi-core systems).
- **Detached**: `spawn` returns `null`, exposes no handle, and never propagates a worker result or failure to its caller. Existing validation and worker diagnostics remain on standard output as `Runtime Error:`, `Thread Error:`, or `Thread Panic:` messages.
- **Argument compatibility**: Since 0.4.0, `spawn` follows the same value semantics as a normal call or `spawn_task`: composite arguments are independent values in the worker. To share mutable state with a worker, use a global with explicit coordination, or channels.

---

## Supervised Tasks

Use `spawn_task(function, ...arguments)` when the caller must observe a routine's result or failure. It validates the Noxy function or closure, its arity, and parameter modes before launch, then returns an opaque task handle. The handle can be stored behind `any`, passed around, printed, and compared by identity; its internal state is not directly accessible.

```noxy
let task: any = spawn_task(calculate, input)

// Wait until the task finishes.
let terminal: any = task_await(task)

// Or wait for at most 500 milliseconds.
let bounded: any = task_await(task, 500)
```

Invalid callables, arity, parameter modes, handles, and timeout values are synchronous runtime errors in the calling VM. The optional timeout is an integer number of milliseconds, must be non-negative, and may be zero for an immediate poll.

Every `task_await` returns a new `map[string, any]` envelope with this contract:

| Status | `value` | `error` |
|---|---|---|
| `"ok"` | The task's return value, including `null` for a void return | `null` |
| `"error"` | `null` | A structured failure map |
| `"timeout"` | `null` | `null` |

An error map contains `kind`, `message`, and `stack`. `kind` is `"runtime"` for a Noxy runtime error and `"panic"` for a recovered internal Go panic. Runtime failures contain a Noxy call stack captured at the failure point; panic failures contain a Go stack. Panic recovery covers the task's main goroutine, not goroutines independently started by native code.

A timeout is non-terminal: it never cancels the worker, consumes its outcome, or changes the task. If completion is observable at the deadline, completion wins over timeout. A later or repeated wait therefore observes the same terminal outcome. Envelopes and error maps are fresh on every wait so their mutation cannot alter the private outcome, while a successful composite `value` preserves its original identity rather than being deep-copied.

Supervised tasks share global/module runtime state and use the normal parameter rules: ordinary composite parameters are independent values through every value field (copy-on-write), while `ref` parameters, `ref` fields and closure upvalues preserve shared identity. Coordinate concurrent access to intentionally shared state (globals, refs, upvalues) with channels or another explicit ownership protocol.

The original `spawn` API remains detached for compatibility. Choose `spawn_task` only when the caller needs structured completion, replayable results, or failures it can inspect.

---

## Channels

Channels are the pipes that connect concurrent routines. You can send values into a channel from one routine and receive them in another.

### Creating a Channel

Use `make_chan(buffer_size)`:

```noxy
// Unbuffered channel (Synchronous)
let c_sync: chan int = make_chan(0)

// Buffered channel (Asynchronous/Queue)
let c_buf: chan string = make_chan(10)

// Untyped channel (can hold any value)
let c_any: chan any = make_chan(5)
chan_send(c_any, 42)
chan_send(c_any, "hello")
```

### Sending and Receiving

- **Send**: `chan_send(channel, value)`
- **Receive**: `chan_recv(channel)`
- **Close**: `chan_close(channel)`
- **Check Closed**: `chan_is_closed(channel)` -> `bool`

```noxy
func sender(c: chan string)
    chan_send(c, "Hello from Routine!")
    chan_close(c)
end

func main()
    let c: chan string = make_chan(0)
    spawn(sender, c)
    
    let msg: string = chan_recv(c) // Blocks until message is received
    print(msg) 
end
```

### Closing Channels

Closing a channel indicates that no more values will be sent. This is useful to signal completion to receivers.

1.  **Sending**: You cannot send to a closed channel (it will panic or error).
2.  **Receiving**:
    *   Receivers can continuing reading values that were already sent (buffered).
    *   Once the channel is empty and closed, `recv` returns `null`.
    *   Looping over a channel often checks for `null` to exit.
3.  **State**: Use `is_closed(ch)` to check the state safely.

```noxy
func producer(c: chan int)
    chan_send(c, 1)
    chan_send(c, 2)
    chan_close(c) // Signal end
end

func main()
    let c: chan int = make_chan(2)
    spawn(producer, c)

    // Consumer loop
    while true do
        // Note: chan_is_closed returns true immediately after close, even if buffer has data.
        // We continue receiving until we get null (EOF).
        let v: any = chan_recv(c) // recv returns 'any' (or type safe if inferred, but null check requires dynamic)
        if v == null then
             if chan_is_closed(c) then
                print("Channel closed and empty")
                break
             end
        end
        print("Received:", v)
    end
end
```

> [!NOTE]
> Since `null` can be a valid value sent over a channel, `recv` returning `null` is ambiguous (it could be a value or EOF).
> - If you send `null` values, consider wrapping them or using a unique sentinel object to distinguish data from EOF.
> - `is_closed(c)` returns `true` as soon as `close` is called, even if the buffer still has data.

### Unbuffered vs Buffered

*   **Unbuffered (`size=0`)**: The sender blocks until the receiver is ready to receive. The receiver blocks until the sender is ready to send. This provides implicit synchronization.
*   **Buffered (`size>0`)**: The sender blocks only if the buffer is full. The receiver blocks only if the buffer is empty.

---

## Advanced Patterns

### Producer-Consumer
One routine generates data, another processes it.

```noxy
func producer(c: chan int)
    let i: int = 0
    while i < 5 do
        chan_send(c, i)
        i = i + 1
    end
    chan_send(c, -1) // Sentinel value (End of Stream)
end

func consumer(c: chan int)
    while true do
        let v: int = chan_recv(c)
        if v == -1 then break end
        print(fmt("Consumed: %d", v))
    end
end
```

### Worker Pool / Parallel Map
Distribute work across multiple cores.

```noxy
use time

func worker(id: int, jobs: chan int, results: chan int)
    while true do
        let job: int = chan_recv(jobs) // Receive payload
        if job == -1 then break end // Exit signal
        
        // Process
        let res: int = job * job
        chan_send(results, res)
    end
end

func main()
    let jobs: chan int = make_chan(100)
    let results: chan int = make_chan(100)
    
    // Start 4 workers
    let w: int = 0
    while w < 4 do
        spawn(worker, w, jobs, results)
        w = w + 1
    end
    
    // Send 10 jobs
    let j: int = 0
    while j < 10 do
        chan_send(jobs, j)
        j = j + 1
    end
    
    // Stop signal
    // ...
end
```

## Multi-Channel Selection (`when`)

The `when` statement (similar to Go's `select`) allows a routine to wait on multiple channel operations simultaneously.

### Syntax

```noxy
when
    case msg = chan_recv(ch1) then
        print("Received: " + msg)
    case chan_send(ch2, "data") then
        print("Sent data")
    default
        print("No channel ready (non-blocking)")
end
```

### Behavior
- **Fairness**: If multiple cases are ready, one is chosen at random.
- **Blocking**: If no `default` case is present, `when` blocks until one of the operations can proceed.
- **Default**: If present and no channel is ready, `default` executes immediately.

### Timeouts (Pattern)
You can implement timeouts by spawning a sleeper routine:

```noxy
let ch: any = make_chan()
let timeout: any = make_chan()

spawn(func(c: any)
    time_sleep(1000)
    chan_send(c, true)
end, timeout)

when
    case msg = chan_recv(ch) then
        print("Received message")
    case chan_recv(timeout) then
        print("Timed out!")
end
```

---

## Synchronization with WaitGroup

While channels are great for communication, sometimes you just want to wait for a set of routines to finish. Noxy provides `WaitGroup` for this pattern.

### Usage

1.  Create with `make_wg()`.
2.  Add work count with `wg_add(wg, n)`.
3.  Routine calls `wg_done(wg)` when finished.
4.  Main calls `wg_wait(wg)` to block.

```noxy
func worker(id: int, wg: any)
    print("Working...")
    wg_done(wg)
end

func main()
    let wg: any = make_wg()
    
    wg_add(wg, 3)
    spawn(worker, 1, wg)
    spawn(worker, 2, wg)
    spawn(worker, 3, wg)
    
    wg_wait(wg) // Main waits here
    print("All tasks finished.")
end
```

## Performance
Noxy routines utilize Go's underlying goroutines. This means:
- Noxy can utilize **all CPU cores** automatically.
- Synchronization overhead is minimal.
- In benchmarks, Noxy's concurrent performance exceeds interpreted languages bounded by a GIL (like Python threads) for CPU-bound tasks.
