-- Loop apertado: despacho de bytecode e aritmetica inteira, sem alocacao.
local function main()
  local i, s = 0, 0
  while i < 3000000 do
    s = (s + i * 3) % 1000003
    i = i + 1
  end
  print("CHECKSUM:" .. s)
end

main()
