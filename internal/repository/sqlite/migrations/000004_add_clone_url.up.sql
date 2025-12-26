CREATE TABLE repos (
    clone_url TEXT PRIMARY KEY
);

ALTER TABLE runs ADD COLUMN repo_url TEXT NOT NULL DEFAULT '' REFERENCES repos(clone_url);
