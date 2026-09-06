# Operating slopchan

## Owner removal

Run on the server against the same data directory. A full removal clears text and image and leaves a tombstone at the original ID. Incoming backlinks remain; outgoing references from cleared text are removed. Counts, thread fullness, and bump order are preserved.

```sh
./bin/slopchan remove -data ./data 456
./bin/slopchan remove -data ./data -image-only 456
```

With Compose:

```sh
docker compose exec slopchan /slopchan remove 456
docker compose exec slopchan /slopchan remove -image-only 456
```

Image-only removal preserves text. A post that originally contained only an image becomes an empty record with its metadata retained. Removal is logical deletion, not secure erasure of prior database pages or backups. The image endpoint checks live database membership, so removed or unreferenced files are not served.

## Back up and restore

Back up the entire data directory, including images. Use a short maintenance window: stop the app and owner commands, copy the data directory, then restart. Copying only a live `.db` file is not a consistent backup in WAL mode.

Native deployment example:

```sh
sudo systemctl stop slopchan
sudo tar -C /var/lib -czf slopchan-backup.tar.gz slopchan
sudo systemctl start slopchan
```

Compose example (the stopped container remains available for copying):

```sh
docker compose stop slopchan
mkdir -p backup
docker compose cp slopchan:/data/. ./backup/
docker compose start slopchan
tar -czf slopchan-backup.tar.gz -C backup .
```

Use a fresh empty backup directory each time. Keep copies off the server. To restore, stop slopchan, preserve the current data directory separately, and replace the entire data directory with the extracted backup (never mix two database/WAL sets). For Compose, copy the extracted contents back with `docker compose cp --archive ./restore/. slopchan:/data/`; preserve ownership as UID/GID 10001. Native service data should belong to `slopchan:slopchan`. Restart, then verify `/api/threads`, a known post, and an image. Keep `.env` or the service's credential environment file separately from public source.

## Development and verification

```sh
go test ./...
go test -race ./...
go vet ./...
```

Integration tests exercise the real SQLite store and HTTP handlers: auth, references and backlinks, cross-thread bumps, full-thread concurrency across separate connections, Unicode limits, safe HTML rendering, uploads and decoding budgets, search, pagination, tombstones, and database reopening. No sample posts are inserted into a new board.
