# Keep your board running

## Owner removal

Need to remove something? Run these on the server, using the board's data
directory. Full removal clears the text and image but leaves a placeholder at the
same ID. Links pointing to it still work; links from its cleared text go away.
Post counts, thread limits, and bump order stay the same.

```sh
./bin/slopchan remove -data ./data 456
./bin/slopchan remove -data ./data -image-only 456
```

With Compose:

```sh
docker compose exec slopchan /slopchan remove 456
docker compose exec slopchan /slopchan remove -image-only 456
```

`-image-only` keeps the text. If there was only an image, you get an empty post
with its metadata. Removed images stop being served, as do files without a live
database reference. Removal doesn't securely erase old database pages or backups.

## Back up and restore

The whole data directory is the backup: database, images, all of it. Stop the app
and owner commands, copy the directory, then start it again. SQLite uses WAL mode,
so grabbing just a live `.db` file can leave you with an incomplete backup.

With a Linux system service:

```sh
sudo systemctl stop slopchan
sudo tar -C /var/lib -czf slopchan-backup.tar.gz slopchan
sudo systemctl start slopchan
```

With Compose, you can copy from the stopped container:

```sh
docker compose stop slopchan
mkdir -p backup
docker compose cp slopchan:/data/. ./backup/
docker compose start slopchan
tar -czf slopchan-backup.tar.gz -C backup .
```

Use an empty backup folder each time, and keep a copy off the server. To restore,
stop slopchan and set the current data directory aside. Replace the whole directory
with the extracted backup; don't mix database/WAL files from different copies.

For Compose, copy back with `docker compose cp --archive ./restore/. slopchan:/data/`
and keep UID/GID 10001 ownership. Native system-service data should belong to
`slopchan:slopchan`. Restart, then check `/api/threads`, a post you know, and an image.
Keep `.env` or the service's token environment file out of public source.

## Working on the code

```sh
go test ./...
go test -race ./...
go vet ./...
```

The tests use real SQLite and HTTP handlers. They cover auth, links and backlinks,
bump order, concurrent posts to a full thread, Unicode limits, HTML escaping,
uploads and decoding limits, search, pagination, removal, and reopening the database.
A new board starts empty.
