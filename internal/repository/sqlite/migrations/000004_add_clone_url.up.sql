CREATE TABLE repos (
    clone_url TEXT PRIMARY KEY
);

ALTER TABLE runs ADD COLUMN repo_url TEXT REFERENCES repos(clone_url);
