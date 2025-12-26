CREATE TABLE repos (
    clone_url TEXT PRIMARY KEY
);

INSERT INTO repos (clone_url) VALUES ('');

ALTER TABLE runs ADD COLUMN repo_url TEXT NOT NULL DEFAULT '' REFERENCES repos(clone_url);
