import time

def main():
    start = time.time() * 1000
    
    i = 0
    total_sum = 0
    while i < 10000000:
        total_sum = total_sum + 1
        i = i + 1
    
    end_time = time.time() * 1000
    print(f"Loop 10M iterations took: {end_time - start:.2f} ms")
    print(f"Sum: {total_sum}")

if __name__ == "__main__":
    main()
