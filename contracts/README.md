# contracts

`openapi.yaml` is the agreement between `core` and `bot`. It lives here rather
than inside either service because neither owns it: a change lands in this
folder, and the diff makes plain that both sides are affected.

## Which side generates

The **TypeScript client is generated**, and that is not optional. Once the bot
leaves Go, a renamed field stops being a compile error and becomes a runtime
surprise months later. Generated types are what buys that safety back; a
hand-written interface drifts silently and turns this file into decoration.

    cd services/bot && npm run gen:api      # arrives with the TS bot

The **Go handlers are written by hand**. Adding a code generator would mean
vendoring its toolchain into a build that deliberately runs offline from
`vendor/`, to police ten routes that sit in one visible table in
`internal/httpapi/api.go`. The cost outweighs the drift it would prevent,
because on this side the spec and the code are in the same repository, in the
same language, reviewed together.

That asymmetry is a judgement, not an oversight. It is only safe because
`internal/httpapi/spec_test.go` reads this file and fails when a route here has
no handler, or is served under a different method. That test is the thing
holding the Go side honest — if it is ever deleted, generate the handlers
instead.

## Conventions

**Dates are ISO.** `2026-09-04`, never `04/09/2026`. The Vietnamese forms are
what people type, and parsing them is the client's job.

**Rendered text sits beside the raw data.** Responses a client would lay out as
a monospace table carry a `table` field holding that table, already drawn.
Which prize has how many numbers of how many digits is domain knowledge, and
the column arithmetic is the part most likely to break when written a second
time in another language. A client that would rather draw its own can ignore
the field.

**Nineteen endpoints, six of which write.** Subscriptions and announcement claims. Everything
else is a read over numbers a public website already publishes, which is why
there is no auth — but those five change what an exposed port would cost.
Silencing the daily announcement, or burning a claim so a result is never
posted, are both a single request away. The listener stays on the compose
network.

**Clients switch on `code`, not on `message`.** The wording is for logs and may
change. Three pairs are deliberately kept apart rather than flattened into one
failure:

- `not_yet` vs `no_draw` — one is worth asking about again, the other never
  will be.
- `already_claimed` vs an error — losing a race for the announcement is an
  ordinary outcome, and the caller's job is to stay quiet, not to recover.
- `not_configured` vs `upstream` — a feature switched off in this deployment is
  not a failure. Flatten it and a person is told "lỗi" when the truth is "chưa
  bật", and goes looking for a problem that is not there.

Each of those distinctions was added because collapsing it produced a visibly
worse answer, not on principle.
