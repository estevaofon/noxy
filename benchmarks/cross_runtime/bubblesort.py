# O(n^2) de leitura/escrita indexada em array, ordenado in-place.
def bubble(data):
    n = len(data)
    i = 0
    while i < n:
        j = 0
        while j < n - i - 1:
            if data[j] > data[j + 1]:
                tmp = data[j]
                data[j] = data[j + 1]
                data[j + 1] = tmp
            j = j + 1
        i = i + 1


def main():
    data = []
    i = 0
    while i < 1200:
        data.append((i * 7919) % 104729)
        i = i + 1
    bubble(data)
    s = 0
    i = 0
    while i < 1200:
        s = s + data[i] * (i % 13)
        i = i + 1
    print(f"CHECKSUM:{s}")


main()
