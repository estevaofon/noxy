import time
import threading
import queue

def is_prime(n):
    if n <= 1: return False
    if n <= 3: return True
    if (n % 2 == 0) or (n % 3 == 0): return False
    i = 5
    while (i * i) <= n:
        if (n % i == 0) or (n % (i + 2) == 0): return False
        i += 6
    return True

def worker(start_n, end_n, q):
    count = 0
    for i in range(start_n, end_n):
        if is_prime(i):
            count += 1
    q.put(count)

def main():
    print("Python Benchmark: Primes Parallel (Threading)")
    start_time = time.time() * 1000
    
    limit = 1000000
    num_threads = 4
    range_size = limit // num_threads
    q = queue.Queue()
    threads = []
    
    for i in range(num_threads):
        s = i * range_size
        e = s + range_size
        t = threading.Thread(target=worker, args=(s, e, q))
        threads.append(t)
        t.start()
        
    for t in threads:
        t.join()
        
    total_primes = 0
    while not q.empty():
        total_primes += q.get()
        
    end_time = time.time() * 1000
    print(f"Total Primes: {total_primes}")
    print(f"Time: {int(end_time - start_time)} ms")

if __name__ == "__main__":
    main()
