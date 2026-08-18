# Aritmetica de ponto flutuante em loop fechado.
def mandel(w: int, h: int, max_iter: int) -> int:
    total = 0
    py = 0
    while py < h:
        y0 = float(py) / float(h) * 2.0 - 1.0
        px = 0
        while px < w:
            x0 = float(px) / float(w) * 3.0 - 2.0
            x = 0.0
            y = 0.0
            it = 0
            while it < max_iter and x * x + y * y <= 4.0:
                xt = x * x - y * y + x0
                y = 2.0 * x * y + y0
                x = xt
                it = it + 1
            total = total + it
            px = px + 1
        py = py + 1
    return total


def main():
    print(f"CHECKSUM:{mandel(200, 200, 50)}")


main()
