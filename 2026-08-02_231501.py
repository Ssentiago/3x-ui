def factorial(n: int) -> int:
    """Возвращает факториал числа n (n!)."""
    if n < 0:
        raise ValueError("Факториал определён только для неотрицательных чисел")
    result = 1
    for i in range(2, n + 1):
        result *= i
    return result


if __name__ == "__main__":
    for x in range(11):
        print(f"{x}! = {factorial(x)}")
