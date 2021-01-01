CREATE TABLE article (
    article_id INT UNSIGNED NOT NULL PRIMARY KEY AUTO_INCREMENT,
    slug VARCHAR(200) NOT NULL UNIQUE,
    published DATETIME NOT NULL,
    hidden TINYINT UNSIGNED NOT NULL DEFAULT 0,
    title TEXT NOT NULL,
    summary_html LONGTEXT NOT NULL,
    full_html LONGTEXT NOT NULL,
    full_plain LONGTEXT NOT NULL,
    FULLTEXT(full_plain),
    FULLTEXT(title)
);

CREATE INDEX by_slug ON article (slug);
CREATE INDEX by_publish_date ON article (published);

CREATE TABLE article_tag (
    article_id INT UNSIGNED NOT NULL REFERENCES article (article_id) ON UPDATE CASCADE ON DELETE CASCADE,
    tag VARCHAR(200) NOT NULL,
    PRIMARY KEY(article_id, tag),
    CONSTRAINT article_fk FOREIGN KEY (article_id) REFERENCES article (article_id) ON UPDATE CASCADE ON DELETE CASCADE
);

CREATE INDEX by_tag ON article_tag (tag);
