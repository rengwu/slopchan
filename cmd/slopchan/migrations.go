package main

import (
	"database/sql"
	"regexp"
	"strings"
)

// Historical schema names are retained here so existing databases can be upgraded.
const schemaV2 = `BEGIN IMMEDIATE;
CREATE TABLE projects (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, slug TEXT NOT NULL UNIQUE, description TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL);
CREATE TABLE threads_v2 (
 id INTEGER PRIMARY KEY REFERENCES posts(id), post_count INTEGER NOT NULL CHECK(post_count>=1),
 last_post_id INTEGER NOT NULL REFERENCES posts(id), bumped_at TEXT NOT NULL,
 project_id INTEGER REFERENCES projects(id), full INTEGER NOT NULL DEFAULT 0
);
INSERT INTO threads_v2(id,post_count,last_post_id,bumped_at,full) SELECT id,post_count,last_post_id,bumped_at,post_count>=200 FROM threads;
DROP TABLE threads;
ALTER TABLE threads_v2 RENAME TO threads;
CREATE INDEX threads_bump ON threads(bumped_at DESC,last_post_id DESC);
CREATE INDEX threads_project ON threads(project_id,bumped_at DESC,last_post_id DESC);
CREATE TABLE settings (id INTEGER PRIMARY KEY CHECK(id=1), public_url TEXT NOT NULL DEFAULT '', post_limit INTEGER NOT NULL DEFAULT %d CHECK(post_limit BETWEEN 1 AND 10000), onboarding_prompt TEXT);
INSERT INTO settings(id) VALUES(1);
CREATE TABLE admin (id INTEGER PRIMARY KEY CHECK(id=1), email TEXT NOT NULL, password_hash TEXT NOT NULL);
CREATE TABLE sessions (hash BLOB PRIMARY KEY, expires_at INTEGER NOT NULL);
CREATE TABLE access_tokens (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, hash BLOB NOT NULL UNIQUE, secret BLOB NOT NULL, created_at TEXT NOT NULL, last_used_at TEXT NOT NULL DEFAULT '', revoked_at TEXT NOT NULL DEFAULT '');
PRAGMA user_version=2;
COMMIT;`

// Upgrade schema v2 in one transaction. IDs, memberships, posts, credentials,
// and settings survive the terminology change.
func migrateBoards(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`
 ALTER TABLE projects RENAME TO boards;
 ALTER TABLE threads RENAME COLUMN project_id TO board_id;
 DROP INDEX threads_project;
 CREATE INDEX threads_board ON threads(board_id,bumped_at DESC,last_post_id DESC);
 `); err != nil {
		return err
	}
	var prompt sql.NullString
	if err = tx.QueryRow(`SELECT onboarding_prompt FROM settings WHERE id=1`).Scan(&prompt); err != nil {
		return err
	}
	if prompt.Valid {
		if _, err = tx.Exec(`UPDATE settings SET onboarding_prompt=? WHERE id=1`, boardTerminology(prompt.String)); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(`PRAGMA user_version=3`); err != nil {
		return err
	}
	return tx.Commit()
}

var legacyBoardWord = regexp.MustCompile(`(?i)\bprojects?\b`)

func boardTerminology(text string) string {
	text = strings.NewReplacer("project_id", "board_id", "project_name", "board_name", "ProjectID", "BoardID", "ProjectName", "BoardName").Replace(text)
	return legacyBoardWord.ReplaceAllStringFunc(text, func(word string) string {
		replacement := "board"
		if strings.HasSuffix(strings.ToLower(word), "s") {
			replacement = "boards"
		}
		if word == strings.ToUpper(word) {
			return strings.ToUpper(replacement)
		}
		if word[0] == 'P' {
			return "B" + replacement[1:]
		}
		return replacement
	})
}
