# HTTP Server

`http_server` implements an HTTP/1.1 origin server that frames each request
incrementally. It reads the header block and the `Content-Length` body to
completion, enforces explicit limits, bounds every phase with a deadline, and
answers an invalid request with a real HTTP response.

The server handles one request per connection and always responds with
`Connection: close`.

## Quick start

```noxy
use http_server select *
use http_parser select *

func handler(req: HttpRequest) -> HttpResponse
    if req.path == "/" then
        return response_text("Hello")
    end
    return response_404()
end

let server: HttpServer = new_server("127.0.0.1", 8080)
serve(ref server, handler)
```

`serve`, `bind_server`, and `stop_server` all take `ref HttpServer`. Since
0.19 a `ref` parameter is fed `ref x` at EVERY call site — there is no
module boundary to distinguish, and no automatic conversion anywhere — see
[`docs/REF_SEMANTICS.md`](REF_SEMANTICS.md), section "Em chamadas".
Omitting it raises `argument 1 to 'serve': expected ref HttpServer, got
HttpServer` with the hint `use 'ref server'`.

## Binding

`bind_server(ref server)` binds the listener without entering the accept loop
and returns whether it succeeded. It writes the real bound port back to
`server.port`, so passing port `0` gives an ephemeral port the caller can
read:

```noxy
let server: HttpServer = new_server("127.0.0.1", 0)
if bind_server(ref server) then
    print("listening on " + to_str(server.port))
    serve(ref server, handler)
end
```

`serve(ref server, handler)` binds automatically when the server is not
already bound, so the single-call form keeps working unchanged.

Binding first and calling `serve` from a spawned routine afterward is the
pattern for learning the bound port before accepting, or for stopping the
server from the routine that started it:

```noxy
func run_server()
    serve(ref server, handler)
end

let server: HttpServer = new_server("127.0.0.1", 0)

func main()
    if !bind_server(ref server) then
        return
    end
    spawn(run_server)
    // server.port is already known here.
    // ...
    stop_server(ref server)
end
```

A plain top-level `let` already creates a binding any function — including a
spawned one — can read and reassign; there is no `global` keyword in Noxy.

## Configuration

Assign the fields before calling `serve`. A non-positive value falls back to its
default, and `read_chunk_bytes` is clamped to `max_header_bytes`.

| Field | Default | Meaning |
|---|---|---|
| `max_header_bytes` | `16384` | maximum size of the header block, terminator included |
| `max_body_bytes` | `1048576` | maximum accepted `Content-Length` |
| `header_timeout_ms` | `5000` | total budget for receiving the header block |
| `body_timeout_ms` | `15000` | total budget for receiving the body |
| `write_timeout_ms` | `15000` | total budget for writing one response |
| `read_chunk_bytes` | `8192` | receive size per socket read |

```noxy
let server: HttpServer = new_server("0.0.0.0", 8080)
server.max_body_bytes = 8388608
server.header_timeout_ms = 3000
serve(ref server, handler)
```

The timeouts are the slowloris defense: they are absolute budgets per phase, so
a client that trickles one byte at a time still hits them.

## Framing contract

- The header block ends at the first `\r\n\r\n`. A lone `\n` is not a line
  terminator.
- The request line must have exactly three space-separated fields. The method
  is an RFC 9110 token of at most 64 characters; the target is origin-form,
  starts with `/`, is at most 2048 characters, and contains only visible ASCII;
  the version is `HTTP/1.0` or `HTTP/1.1`.
- A header name is a token of at most 256 characters with no whitespace before
  the `:`. Obsolete line folding is rejected rather than joined. At most 64
  header lines are accepted; the 65th is an error rather than a silent drop.
- A header line may not contain a control character. Horizontal tab is allowed,
  as is any character from `0x20` up excluding `0x7F`; `CR`, `LF`, `NUL`, and
  `DEL` are rejected. This is what stops response splitting: `CR` and `LF` are
  valid UTF-8, so a header value carrying them would otherwise reach a handler
  intact and forge a second response the moment the handler echoed it back.
- A header value keeps every character after the first `:`, so
  `Host: example.com:8080` is read whole.
- The header block must be valid UTF-8. Noxy strings are required to hold valid
  UTF-8, so a header carrying a raw non-UTF-8 byte is rejected at the boundary
  rather than decoded into something the client did not send.
- The body length comes from `Content-Length` only. It must be 1 to 19 ASCII
  digits with no sign, decimal point, or list, and must fit in an `int`. A
  request with neither `Content-Length` nor `Transfer-Encoding` has no body,
  including a `POST`.
- `Transfer-Encoding` is not implemented; chunked requests are refused rather
  than mis-framed.
- Bytes arriving after the declared body length are discarded, because the
  connection closes after one response.

## Status codes for invalid requests

| Status | Condition |
|---|---|
| `400 Bad Request` | header block that is not valid UTF-8, control character in a header line, malformed request line or header, obsolete folding, whitespace before `:`, non-origin target, duplicate or non-digit `Content-Length`, a `Content-Length` too large to represent, `Transfer-Encoding` with `Content-Length`, connection ended mid-request |
| `408 Request Timeout` | the header or body budget expired |
| `413 Content Too Large` | declared `Content-Length` above `max_body_bytes` |
| `414 URI Too Long` | target longer than 2048 characters |
| `431 Request Header Fields Too Large` | header block at or above `max_header_bytes`, or more than 64 header lines |
| `501 Not Implemented` | `Transfer-Encoding` present |
| `505 HTTP Version Not Supported` | version other than `HTTP/1.0` or `HTTP/1.1` |

Every error response carries `Content-Type: text/plain`, a byte-exact
`Content-Length`, `Connection: close`, and the reason as its body. A connection
that closes before any request byte arrives receives no response at all.

## Responses

`response_ok`, `response_text`, `response_json`, `response_html`,
`response_error`, `response_404`, `response_500`, and `serve_static` keep their
signatures. Every response is written completely: a partial write resumes from
the transferred offset until the response is sent or `write_timeout_ms`
expires.

The connection is closed through `defer`, so a runtime error raised inside a
handler closes the socket instead of leaking it.

## Handling request bodies

`req.body` is `bytes`, and the server does not inspect it — a body may legally
carry any byte, including a PNG or a gzip stream. Noxy strings must hold valid
UTF-8, so `to_str` raises when the body is not text:

```noxy
use strings select *

func handler(req: HttpRequest) -> HttpResponse
    if !is_valid_utf8(req.body) then
        return response_error(400, "Body must be UTF-8 text")
    end
    return response_text(to_str(req.body))
end
```

A handler that raises does not take the server down — `defer` closes the
socket and the accept loop keeps running — but that client receives no
response. Gate the conversion when the body may not be text.

## Stopping a server

`stop_server(ref server)` closes the listener, which unblocks the accept loop
with an error and ends `serve`. Calling it from a different routine than the
one running `serve` — the usual case, since `serve` blocks — is the intended
usage; `serve`'s accept loop reacts to the closed listener rather than polling
`server.running`; that field is written for informational purposes but nothing
in this module reads it back.

## Not supported

Keep-alive, request pipelining, chunked `Transfer-Encoding`,
`Expect: 100-continue`, HTTP/2, and TLS.
