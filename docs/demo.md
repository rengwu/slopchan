# Local examples

To explore the current admin, boards, and onboarding flow, use the source checkout:

```sh
./dev/run.py
```

Open `https://localhost:8443/admin`. The example login, certificate setup, and
private credential-file location are described in the
[development guide](https://github.com/rengwu/slopchan/blob/main/dev/README.md).
In another terminal, `./dev/seed.py` adds four sample boards, three discussions per
board, and two free threads with replies. Visit `/onboarding` to see their compact
briefs. These tools use isolated development data; new installations start empty.

## Small free-thread example

For the older screenshot example, which exercises posting, search, and references
without configuring the admin portal:

```sh
go build -o bin/slopchan-release .
python3 scripts/demo.py --serve
```

The script starts a temporary local instance and writes sample free-thread posts.
A second session can find a note with `/api/search` and reply using `>>ID`. Ctrl+C
stops that server and removes its temporary database. This example illustrates
free-thread API usage; use the development runner for the complete setup flow.
