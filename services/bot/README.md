# bot

The Discord client. It holds no database and no scraper: every number it shows
comes from `core` over the contract in `../../contracts/openapi.yaml`.

## Running the tests

    npm install
    npm run gen:api      # regenerate src/core/schema.d.ts from the contract
    npm run check        # tsc --noEmit
    npm test             # tsc, then node --test on the compiled output

Tests run on Node's built-in runner, so there is no test framework in the
dependency tree. That is not minimalism for its own sake: vitest pulls in vite,
which pulls in rollup and esbuild, which ship **per-platform native binaries**.
A lockfile written on Linux then installs the wrong binary on Windows and npm
reports it as a corrupt module. Nothing here needs more than `assert`, and
avoiding that class of problem entirely is worth more than nicer matchers.

`test/core.test.ts` talks to a real core rather than a mock. A mock would agree
with whatever the test author believed the contract said, which is the one
thing not worth checking once two languages read the same document.

    cd ../core && go run ./cmd/fakecore :8099          # in another terminal
    cd ../core && go run ./cmd/fakecore :8098 gold     # and a third, for gold
    CORE_REQUIRED=1 GOLD_CORE_URL=http://127.0.0.1:8098 npm test

Gold is off in the default fakecore on purpose, so the ordinary run exercises
the `not_configured` path every client has to handle. The second instance with
`gold` covers the rendering.

The runner takes one file at a time (`--test-concurrency=1`). The files share a
core, and therefore share its database; one of them clears every subscription,
which would otherwise wipe rows another file is asserting on.

Without a core those tests are **skipped**, and the runner says so. They are never
quietly passed: a green suite that checked nothing is worse than no suite, and
the first version of this file got that wrong.

`CORE_REQUIRED=1` turns an unreachable core into a hard failure. CI sets it.

## Regenerating the client

`src/core/schema.d.ts` is generated. Do not edit it, and do not hand-write the
types beside it either. In Go a renamed field was a compile error; here the
generator is the only thing that puts that back, and a hand-written interface
drifts in silence.

## About package-lock.json

Commit it. But note that `npm ci` with a lockfile written on another platform
is where the native-binary problem above comes from. Since the dependency tree
now has no native binaries at all, that risk is gone — keep it that way, and
think twice before adding a dependency that ships a `.node` file.
