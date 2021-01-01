package article

import (
	"bufio"
	"database/sql"
	"errors"
	"html"
	"io"
	"os"
	"path"
	"regexp"
	"strings"
	"time"

	"code.laria.me/laria.me/dbutils"
	"code.laria.me/laria.me/markdown"
)

var (
	ErrBrokenHeader            = errors.New("The article header is broken")
	ErrMissingMandatoryHeaders = errors.New("The article header is missing some mandatory headers")
)

type Article struct {
	Slug        string
	Published   time.Time
	Hidden      bool
	Title       string
	SummaryHtml string
	FullHtml    string
	Tags        map[string]struct{}
}

var reHtmlTag = regexp.MustCompile(`<([^>'"]+|'[^']*'|"[^"]*")*>`)
var reHtmlEntity = regexp.MustCompile(`&[^;]*;`)

func stripTags(s string) string {
	s = reHtmlTag.ReplaceAllString(s, "")
	s = reHtmlEntity.ReplaceAllStringFunc(s, html.UnescapeString)
	return s
}

func (a Article) updateDbDetails(tx *sql.Tx, id int64) error {
	_, err := tx.Exec(`
		UPDATE article SET
			published = ?,
			hidden = ?,
			title = ?,
			summary_html = ?,
			full_html = ?,
			full_plain = ?
		WHERE article_id = ?
	`, a.Published.Format("2006-01-02 15:04:05"), a.Hidden, a.Title, a.SummaryHtml, a.FullHtml, stripTags(a.FullHtml), id)

	return err
}

func (a Article) createInDb(tx *sql.Tx) (int64, error) {
	res, err := tx.Exec(`
		INSERT INTO article SET
			slug = ?,
			published = ?,
			hidden = ?,
			title = ?,
			summary_html = ?,
			full_html = ?,
			full_plain = ?
	`, a.Slug, a.Published.Format("2006-01-02 15:04:05"), a.Hidden, a.Title, a.SummaryHtml, a.FullHtml, stripTags(a.FullHtml))

	if err != nil {
		return 0, err
	}

	return res.LastInsertId()
}

func removeTags(tx *sql.Tx, id int64) error {
	_, err := tx.Exec(`DELETE FROM article_tag WHERE article_id = ?`, id)
	return err
}

func setTags(tx *sql.Tx, id int64, tags map[string]struct{}) error {
	stmt, err := tx.Prepare(`REPLACE INTO article_tag SET article_id = ?, tag = ?`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for tag := range tags {
		if _, err = stmt.Exec(id, tag); err != nil {
			return err
		}
	}

	return nil
}

func (a Article) saveToDb(tx *sql.Tx) (int64, error) {
	var id int64
	switch err := tx.QueryRow(`SELECT article_id FROM article WHERE slug = ?`, a.Slug).Scan(&id); err {
	case nil:
		if err = a.updateDbDetails(tx, id); err != nil {
			return 0, err
		}
	case sql.ErrNoRows:
		id, err = a.createInDb(tx)
		if err != nil {
			return 0, err
		}
	default:
		return 0, err
	}

	if err := removeTags(tx, id); err != nil {
		return 0, err
	}

	if err := setTags(tx, id, a.Tags); err != nil {
		return 0, err
	}

	return id, nil
}

func (a Article) SaveToDb(db *sql.DB) (int64, error) {
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}

	id, err := a.saveToDb(tx)
	err = dbutils.TxCommitIfOk(tx, err)
	return id, err
}

func DeleteArticlesFromDbExcept(db *sql.DB, slugs []string) error {
	if len(slugs) == 0 {
		return nil
	}

	query := new(strings.Builder)
	query.WriteString("DELETE FROM article WHERE slug NOT IN (?")

	for i := 1; i < len(slugs); i++ { // intentionally starting at 1, since the first '?' is already there
		query.WriteString(",?")
	}

	query.WriteString(")")

	slugsAsInterfaces := make([]interface{}, 0, len(slugs))
	for _, slug := range slugs {
		slugsAsInterfaces = append(slugsAsInterfaces, interface{}(slug))
	}

	_, err := db.Exec(query.String(), slugsAsInterfaces...)
	return err
}

func splitTags(s string) map[string]struct{} {
	tags := make(map[string]struct{})

	parts := strings.Split(s, ",")
	for _, part := range parts {
		tag := strings.TrimSpace(part)
		if tag != "" {
			tags[tag] = struct{}{}
		}
	}

	return tags
}

func parseDate(s string) (time.Time, error) {
	return time.Parse("2006-01-02 15:04:05", s)
}

func parseHeader(scanner *bufio.Scanner) (Article, error) {
	var article Article
	var err error

	seenTitle := false
	seenPublished := false

	for scanner.Scan() {
		line := scanner.Text()
		line = strings.TrimSpace(line)

		if line == "" {
			break
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			return Article{}, ErrBrokenHeader
		}

		key := strings.ToLower(strings.TrimSpace(parts[0]))
		value := strings.TrimSpace(parts[1])

		switch key {
		case "title":
			article.Title = value
			seenTitle = true
		case "tags":
			article.Tags = splitTags(value)
		case "date":
			if article.Published, err = parseDate(value); err != nil {
				return Article{}, err
			}
			seenPublished = true
		case "hidden":
			article.Hidden = strings.ToLower(value) == "yes"
		}
	}

	if err = scanner.Err(); err != nil {
		return Article{}, err
	}

	if !seenTitle || !seenPublished {
		return Article{}, ErrMissingMandatoryHeaders
	}

	return article, nil
}

var reMore = regexp.MustCompile(`^\s*~~+(?i:more)~~+\s*$`)

func parseText(scanner *bufio.Scanner, article *Article) error {
	var err error

	builder := new(strings.Builder)

	for scanner.Scan() {
		line := scanner.Text()

		if reMore.MatchString(line) {
			if article.SummaryHtml, err = markdown.Parse(builder.String()); err != nil {
				return err
			}

			continue
		}

		builder.WriteString(line)
		builder.WriteRune('\n')
	}

	if err = scanner.Err(); err != nil {
		return err
	}

	if article.FullHtml, err = markdown.Parse(builder.String()); err != nil {
		return err
	}

	return nil
}

func ParseArticle(r io.Reader) (Article, error) {
	var article Article

	scanner := bufio.NewScanner(r)
	article, err := parseHeader(scanner)
	if err != nil {
		return Article{}, err
	}

	if err = parseText(scanner, &article); err != nil {
		return Article{}, err
	}

	return article, nil
}

func LoadArticle(filename string) (Article, error) {
	parts := strings.Split(path.Base(filename), ".")
	slug := strings.Join(parts[:len(parts)-1], ".")

	f, err := os.Open(filename)
	if err != nil {
		return Article{}, err
	}
	defer f.Close()

	article, err := ParseArticle(f)
	if err != nil {
		return Article{}, err
	}

	article.Slug = slug
	return article, nil
}
