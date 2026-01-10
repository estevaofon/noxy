# Noxy Concurrency Guide

Noxy provides first-class support for concurrency through **Noxy Routines** and **Channels**. Inspired by Go's goroutines and CSP (Communicating Sequential Processes) model, Noxy allows you to write concurrent programs that are simple, safe, and highly performant.

## core Concepts

1.  **Noxy Routines**: Lightweight threads managed by the Noxy Runtime (VM). They are cheap to create (much cheaper than OS threads) and are multiplexed onto OS threads.
2.  **Channels**: Typed conduits that allow Noxy routines to communicate with each other and synchronize their execution.
3.  **Shared State**: While channels are preferred, Noxy routines share the same global variables and module state, allowing for shared-memory patterns (though message passing is recommended).

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
- **Independent**: If a routine crashes (panics), it prints an error but (usually) doesn't kill the whole VM immediately (implementation details verify this).

---

## Channels

Channels are the pipes that connect concurrent routines. You can send values into a channel from one routine and receive them in another.

### Creating a Channel

Use `make_chan(buffer_size)`:

```noxy
// Unbuffered channel (Synchronous)
let c_sync: any = make_chan(0)

// Buffered channel (Asynchronous/Queue)
let c_buf: any = make_chan(10)
```

### Sending and Receiving

- **Send**: `send(channel, value)`
- **Receive**: `recv(channel)`

```noxy
func sender(c: any)
    send(c, "Hello from Routine!")
end

func main()
    let c: any = make_chan(0)
    spawn(sender, c)
    
    let msg: any = recv(c) // Blocks until message is received
    print(msg) 
end
```

### Unbuffered vs Buffered

*   **Unbuffered (`size=0`)**: The sender blocks until the receiver is ready to receive. The receiver blocks until the sender is ready to send. This provides implicit synchronization.
*   **Buffered (`size>0`)**: The sender blocks only if the buffer is full. The receiver blocks only if the buffer is empty.

---

## Advanced Patterns

### Producer-Consumer
One routine generates data, another processes it.

```noxy
func producer(c: any)
    let i: int = 0
    while i < 5 do
        send(c, i)
        i = i + 1
    end
    send(c, -1) // Sentinel value (End of Stream)
end

func consumer(c: any)
    while true do
        let v: int = int(recv(c)) // or to_int
        if v == -1 then break end
        print(fmt("Consumed: %d", v))
    end
end
```

### Worker Pool / Parallel Map
Distribute work across multiple cores.

```noxy
use time

func worker(id: int, jobs: any, results: any)
    while true do
        let job: int = recv(jobs) // Receive payload
        if job == -1 then break end // Exit signal
        
        // Process
        let res: int = job * job
        send(results, res)
    end
end

func main()
    let jobs: any = make_chan(100)
    let results: any = make_chan(100)
    
    // Start 4 workers
    let w: int = 0
    while w < 4 do
        spawn(worker, w, jobs, results)
        w = w + 1
    end
    
    // Send 10 jobs
    let j: int = 0
    while j < 10 do
        send(jobs, j)
        j = j + 1
    end
    
    // Stop signal
    // ...
end
```

## Performance
Noxy routines utilize Go's underlying goroutines. This means:
- Noxy can utilize **all CPU cores** automatically.
- Synchronization overhead is minimal.
- In benchmarks, Noxy's concurrent performance exceeds interpreted languages bounded by a GIL (like Python threads) for CPU-bound tasks.
