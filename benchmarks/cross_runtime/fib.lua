-- Recursao pura: custo de chamada de funcao + aritmetica de inteiros.
local function fib(n)
  if n <= 1 then return n end
  return fib(n - 1) + fib(n - 2)
end

local function main()
  print("CHECKSUM:" .. fib(30))
end

main()
