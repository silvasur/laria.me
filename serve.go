package main

import (
	"bytes"
	"database/sql"
	"encoding/xml"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/go-sql-driver/mysql"
	"github.com/gorilla/mux"

	"code.laria.me/laria.me/atom"
	"code.laria.me/laria.me/dbutils"
	"code.laria.me/laria.me/environment"
	"code.laria.me/laria.me/markdown"
	"code.laria.me/laria.me/menu"
)

type serveContext struct {
	env     *environment.Env
	rwMutex *sync.RWMutex
	pages   map[string]template.HTML
	menu    *menu.Menu
	views   Views
}

func newServeContext(env *environment.Env) (*serveContext, error) {
	context := &serveContext{
		env:     env,
		rwMutex: new(sync.RWMutex),
		pages:   make(map[string]template.HTML),
	}
	if err := context.update(); err != nil {
		return nil, err
	}
	return context, nil
}

var rePageName = regexp.MustCompile(`^(?:[^/]*/)*([^\.]+).*$`)

func pageName(filename string) string {
	m := rePageName.FindStringSubmatch(filename)
	if m == nil {
		return ""
	}

	return m[1]
}

func loadPage(filename string) (template.HTML, error) {
	f, err := os.Open(filename)
	if err != nil {
		return "", err
	}
	defer f.Close()

	buf := new(bytes.Buffer)

	if _, err := io.Copy(buf, f); err != nil {
		return "", err
	}

	html, err := markdown.Parse(buf.String())
	return template.HTML(html), err
}

func readPages(pagesPath string) (map[string]template.HTML, error) {
	f, err := os.Open(pagesPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	infos, err := f.Readdir(-1)
	if err != nil {
		return nil, err
	}

	pages := make(map[string]template.HTML)
	for _, info := range infos {
		if info.IsDir() {
			continue
		}

		name := pageName(info.Name())
		if name == "" {
			continue
		}

		html, err := loadPage(filepath.Join(pagesPath, info.Name()))
		if err != nil {
			return nil, err
		}

		pages[name] = html
	}

	return pages, nil
}

func (ctx *serveContext) update() error {
	conf, err := ctx.env.Config()
	if err != nil {
		return err
	}

	menuPath := path.Join(conf.ContentRoot, "menu.json")
	menu, err := menu.LoadFromFile(menuPath)
	if err != nil {
		return fmt.Errorf("Failed loading menu %s: %w", menuPath, err)
	}

	pagesPath := path.Join(conf.ContentRoot, "pages")
	pages, err := readPages(pagesPath)
	if err != nil {
		return fmt.Errorf("Failed loading pages from %s: %w", pagesPath, err)
	}

	views, err := LoadViews(conf.TemplatePath)
	if err != nil {
		return fmt.Errorf("Failed loading templates: %w", err)
	}

	ctx.rwMutex.Lock()
	defer ctx.rwMutex.Unlock()

	ctx.menu = menu
	ctx.pages = pages
	ctx.views = views

	return nil
}

func (ctx *serveContext) handleUpdate(w http.ResponseWriter, r *http.Request) {
	conf, err := ctx.env.Config()
	if err != nil {
		panic(err)
	}

	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}

	if err := r.ParseForm(); err != nil {
		log.Printf("Could not ParseForm: %s", err)
		w.WriteHeader(500)
		return
	}

	if r.PostForm.Get("secret") != conf.Secret {
		w.WriteHeader(401)
	}

	if err := ctx.update(); err != nil {
		log.Printf("Could not update: %s", err)
		w.WriteHeader(500)
		return
	}

	w.WriteHeader(200)
}

var reHtmlHeadline = regexp.MustCompile(`<\s*h\d\b`)

func rewriteHeadlines(html template.HTML, sub int) template.HTML {
	if sub == 0 {
		return html
	}

	str := string(html)
	str = reHtmlHeadline.ReplaceAllStringFunc(str, func(s string) string {
		_n, _ := strconv.ParseInt(s[len(s)-1:], 10, 8)
		n := int(_n)
		n -= sub
		if n < 1 {
			n = 1
		}
		if n > 6 {
			n = 6
		}

		return fmt.Sprintf("<h%d", n)
	})
	return template.HTML(str)
}

// viewArticlesFromDb creates ViewArticles from an SQL query.
// The quey should select these values: ID, Published, Slug, Title, Content, ReadMore
func viewArticlesFromDb(db *sql.DB, headlineSub int, query string, args ...interface{}) ([]ViewArticle, int, error) {
	tx, err := db.Begin()
	if err != nil {
		return nil, 0, err
	}

	articles, count, err := viewArticlesFromDbTx(tx, headlineSub, query, args...)
	err = dbutils.TxCommitIfOk(tx, err)
	return articles, count, err
}

func viewArticlesFromDbTx(tx *sql.Tx, headlineSub int, query string, args ...interface{}) ([]ViewArticle, int, error) {
	rows, err := tx.Query(query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	ids := make([]int, 0)
	articlesById := make(map[int]*ViewArticle)
	for rows.Next() {
		var va ViewArticle
		var id int

		var t mysql.NullTime

		if err := rows.Scan(
			&id,
			&t,
			&va.Slug,
			&va.Title,
			&va.Content,
			&va.ReadMore,
		); err != nil {
			return nil, 0, err
		}

		if t.Valid {
			va.Published = t.Time
		}

		va.Content = rewriteHeadlines(va.Content, headlineSub)

		ids = append(ids, id)
		articlesById[id] = &va
	}

	if err = rows.Err(); err != nil {
		return nil, 0, err
	}

	var total int
	err = tx.QueryRow(`SELECT FOUND_ROWS()`).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	if len(ids) > 0 {
		inSql, inArgs := dbutils.BuildInIdsSqlAndArgs("article_id", ids)

		rows, err := tx.Query(`
			SELECT article_id, tag
			FROM article_tag
			WHERE `+inSql+`
			ORDER BY article_id, tag
		`, inArgs...)

		if err != nil {
			return nil, 0, err
		}
		defer rows.Close()

		for rows.Next() {
			var id int
			var tag string

			if err := rows.Scan(&id, &tag); err != nil {
				return nil, 0, err
			}

			article, ok := articlesById[id]
			if ok {
				article.Tags = append(article.Tags, tag)
			}
		}

		if err := rows.Err(); err != nil {
			return nil, 0, err
		}
	}

	viewArticles := make([]ViewArticle, 0, len(ids))
	for _, id := range ids {
		viewArticles = append(viewArticles, *articlesById[id])
	}

	return viewArticles, total, nil
}

type responseWriterWithHeaderSentFlag struct {
	http.ResponseWriter
	headersSent bool
}

func (w *responseWriterWithHeaderSentFlag) Write(p []byte) (int, error) {
	w.headersSent = true
	return w.ResponseWriter.Write(p)
}

func (w *responseWriterWithHeaderSentFlag) WriteHeader(statusCode int) {
	w.headersSent = true
	w.ResponseWriter.WriteHeader(statusCode)
}

var errNotFound = errors.New("not found")

func wrapHandleFunc(
	name string,
	f func(http.ResponseWriter, *http.Request) error,
) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		wWrap := &responseWriterWithHeaderSentFlag{w, false}

		err := f(wWrap, r)
		switch err {
		case nil:
			return
		case errNotFound:
			// TODO: A better 404 page
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(404)
			if _, err = fmt.Fprintln(w, "404 Not Found"); err != nil {
				log.Printf("%s: Failed sending 404: %s", name, err)
			}
		default:
			log.Printf("%s: %s", name, err)
			if !wWrap.headersSent {
				w.Header().Set("Content-Type", "text/plain")
				w.WriteHeader(500)
				if _, err = fmt.Fprintln(w, "500 Internal Server Error"); err != nil {
					log.Printf("%s: Failed sending 500: %s", name, err)
				}
			}
		}
	}
}

func (ctx *serveContext) handleArticle(w http.ResponseWriter, r *http.Request) error {
	db, err := ctx.env.DB()
	if err != nil {
		return err
	}

	vars := mux.Vars(r)
	year, _ := strconv.Atoi(vars["year"])
	month, _ := strconv.Atoi(vars["month"])
	day, _ := strconv.Atoi(vars["day"])
	slug := vars["slug"]

	articles, _, err := viewArticlesFromDb(db, 0, `
		SELECT article_id, published, slug, title, full_html, 0 AS ReadMore
		FROM article
		WHERE
			slug = ?
			AND YEAR(published) = ?
			AND MONTH(published) = ?
			AND DAY(published) = ?
			AND NOT hidden
	`, slug, year, month, day)

	if err != nil {
		return err
	}

	if len(articles) != 1 {
		return errNotFound
	}

	return ctx.views.RenderArticle(w, ctx.menu, "blog", articles[0])
}

func (ctx *serveContext) handleArticleQuicklink(w http.ResponseWriter, r *http.Request) error {
	db, err := ctx.env.DB()
	if err != nil {
		return err
	}

	vars := mux.Vars(r)
	slug := vars["slug"]

	articles, _, err := viewArticlesFromDb(db, 0, `
		SELECT article_id, published, slug, title, full_html, 0 AS ReadMore
		FROM article
		WHERE
			slug = ?
			AND NOT hidden
	`, slug)

	if err != nil {
		return err
	}

	if len(articles) != 1 {
		return errNotFound
	}

	article := articles[0]
	y, m, d := article.Published.Date()

	w.Header().Set("Location", fmt.Sprintf("/blog/%d/%d/%d/%s", y, m, d, slug))
	w.WriteHeader(301)
	return nil
}

func (ctx *serveContext) handleArchiveDay(w http.ResponseWriter, r *http.Request) error {
	db, err := ctx.env.DB()
	if err != nil {
		return err
	}

	vars := mux.Vars(r)
	year, _ := strconv.Atoi(vars["year"])
	month, _ := strconv.Atoi(vars["month"])
	day, _ := strconv.Atoi(vars["day"])

	articles, _, err := viewArticlesFromDb(db, 1, `
		SELECT
			article_id,
			published,
			slug,
			title,
			IF(summary_html = '', full_html, summary_html),
			summary_html != '' AS ReadMore
		FROM article
		WHERE
			YEAR(published) = ?
			AND MONTH(published) = ?
			AND DAY(published) = ?
			AND NOT hidden
		ORDER BY published ASC
	`, year, month, day)

	if err != nil {
		return err
	}

	return ctx.views.RenderArchiveDay(w, ctx.menu, "archive", year, month, day, articles)
}

func countArticlesBy(db *sql.DB, byExpr, whereExpr string, whereArgs ...interface{}) (map[int]int, error) {
	rows, err := db.Query(`
		SELECT `+byExpr+`, COUNT(*)
		FROM article
		WHERE `+whereExpr+` AND NOT hidden
		GROUP BY `+byExpr+`
	`, whereArgs...)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := make(map[int]int)
	for rows.Next() {
		var k, v int
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}

		counts[k] = v
	}

	return counts, nil
}

func (ctx *serveContext) handleArchiveMonth(w http.ResponseWriter, r *http.Request) error {
	db, err := ctx.env.DB()
	if err != nil {
		return err
	}

	vars := mux.Vars(r)
	year, _ := strconv.Atoi(vars["year"])
	month, _ := strconv.Atoi(vars["month"])

	counts, err := countArticlesBy(db, "DAY(published)", "YEAR(published) = ? AND MONTH(published) = ?", year, month)
	if err != nil {
		return err
	}

	return ctx.views.RenderArchiveMonth(w, ctx.menu, "archive", year, month, counts)
}

func (ctx *serveContext) handleArchiveYear(w http.ResponseWriter, r *http.Request) error {
	db, err := ctx.env.DB()
	if err != nil {
		return err
	}

	vars := mux.Vars(r)
	year, _ := strconv.Atoi(vars["year"])

	counts, err := countArticlesBy(db, "MONTH(published)", "YEAR(published) = ?", year)
	if err != nil {
		return err
	}

	return ctx.views.RenderArchiveYear(w, ctx.menu, "archive", year, counts)
}

func (ctx *serveContext) handleArchive(w http.ResponseWriter, r *http.Request) error {
	db, err := ctx.env.DB()
	if err != nil {
		return err
	}

	counts, err := countArticlesBy(db, "YEAR(published)", "1")
	if err != nil {
		return err
	}

	return ctx.views.RenderArchive(w, ctx.menu, "archive", counts)
}

const articles_per_page = 30

func getPageArgument(r *http.Request) int {
	vals, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return 1
	}

	if _, ok := vals["page"]; !ok {
		return 1
	}

	rawPage := vals.Get("page")
	page, err := strconv.ParseInt(rawPage, 10, 32)
	if err != nil {
		return 1
	}

	if page < 1 {
		return 1
	}

	return int(page)
}

func calcPages(total int) int {
	return int(math.Ceil(float64(total) / articles_per_page))
}

func (ctx *serveContext) handleTag(w http.ResponseWriter, r *http.Request) error {
	db, err := ctx.env.DB()
	if err != nil {
		return err
	}

	vars := mux.Vars(r)
	tag := vars["tag"]

	page := getPageArgument(r)

	articles, total, err := viewArticlesFromDb(db, 1, `
		SELECT SQL_CALC_FOUND_ROWS
			a.article_id,
			a.published,
			a.slug,
			a.title,
			IF(a.summary_html = '', a.full_html, a.summary_html),
			a.summary_html != '' AS ReadMore
		FROM article_tag t
		INNER JOIN article a
			ON a.article_id = t.article_id
		WHERE
			t.tag = ?
			AND NOT a.hidden
		ORDER BY published DESC
		LIMIT ? OFFSET ?
	`, tag, articles_per_page, (page-1)*articles_per_page)

	if err != nil {
		return err
	}

	pages := calcPages(total)
	return ctx.views.RenderTag(w, ctx.menu, "tags", tag, articles, pages, page)
}

func countTags(db *sql.DB) (map[string]int, error) {
	rows, err := db.Query(`
		SELECT
			at.tag,
			COUNT(*)
		FROM article_tag at
		INNER JOIN article a
			ON a.article_id = at.article_id
		WHERE NOT a.hidden
		GROUP BY at.tag
	`)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var k string
		var v int

		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}

		counts[k] = v
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return counts, nil
}

func (ctx *serveContext) handleTags(w http.ResponseWriter, r *http.Request) error {
	db, err := ctx.env.DB()
	if err != nil {
		return err
	}

	counts, err := countTags(db)
	if err != nil {
		return err
	}

	return ctx.views.RenderTags(w, ctx.menu, "tags", counts)
}

func getSearchQueryArgument(r *http.Request) string {
	vals, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return ""
	}

	if _, ok := vals["q"]; !ok {
		return ""
	}

	q := vals.Get("q")

	return strings.TrimSpace(q)
}

func (ctx *serveContext) handleSearch(w http.ResponseWriter, r *http.Request) error {
	db, err := ctx.env.DB()
	if err != nil {
		return err
	}

	q := getSearchQueryArgument(r)
	page := getPageArgument(r)

	articles := []ViewArticle{}
	total := 0

	if q != "" {
		articles, total, err = viewArticlesFromDb(db, 1, `
			SELECT SQL_CALC_FOUND_ROWS
				article_id,
				published,
				slug,
				title,
				IF(summary_html = '', full_html, summary_html),
				summary_html != '' AS ReadMore
			FROM article
			WHERE
				(MATCH(full_plain) AGAINST(?) OR MATCH(title) AGAINST (?))
				AND NOT hidden
			ORDER BY published DESC
			LIMIT ? OFFSET ?
		`, q, q, articles_per_page, (page-1)*articles_per_page)

		if err != nil {
			return err
		}
	}

	return ctx.views.RenderSearch(
		w,
		ctx.menu,
		"search",
		q,
		total,
		articles,
		calcPages(total),
		page,
	)
}

func (ctx *serveContext) getBlogData(limit, offset int) ([]ViewArticle, int, error) {
	db, err := ctx.env.DB()
	if err != nil {
		return nil, 0, err
	}

	return viewArticlesFromDb(db, 1, `
		SELECT SQL_CALC_FOUND_ROWS
			article_id,
			published,
			slug,
			title,
			IF(summary_html = '', full_html, summary_html),
			summary_html != '' AS ReadMore
		FROM article
		WHERE NOT hidden
		ORDER BY published DESC
		LIMIT ? OFFSET ?
	`, limit, offset)
}

const numFeedEntries = 30

func (ctx *serveContext) handleFeed(w http.ResponseWriter, r *http.Request) error {
	articles, _, err := ctx.getBlogData(numFeedEntries, 0)

	if err != nil {
		return err
	}

	entries := make([]atom.Entry, 0, len(articles))
	for _, article := range articles {
		y, m, d := article.Published.Date()
		url := fmt.Sprintf("http://laria.me/blog/%d/%d/%d/%s", y, m, d, article.Slug)
		entries = append(entries, atom.Entry{
			Title:   article.Title,
			Id:      url,
			Updated: article.Published, // TODO: Or should modification time be tracked?
			Summary: atom.Summary{
				Type:    "html",
				Content: string(article.Content),
			},
			Links: []atom.Link{
				atom.Link{Rel: "alternate", Href: url},
			},
		})
	}

	feed := atom.Feed{
		Title: "laria.me Blog",
		Links: []atom.Link{
			atom.Link{Href: "http://laria.me/"},
			atom.Link{Href: "http://laria.me/blog/feed.xml", Rel: "self"},
		},
		Id:          "http://laria.me/blog",
		AuthorName:  "Laria Carolin Chabowski",
		AuthorEmail: "laria-blog@laria.me",
		AuthorUri:   "http://laria.me",
		Updated:     articles[0].Published, // TODO: This should be able to deal with articles being empty
		Entries:     entries,
	}

	return xml.NewEncoder(w).Encode(feed)
}

func (ctx *serveContext) handleBlog(w http.ResponseWriter, r *http.Request) error {
	page := getPageArgument(r)

	articles, total, err := ctx.getBlogData(articles_per_page, (page-1)*articles_per_page)

	if err != nil {
		return err
	}

	pages := calcPages(total)
	return ctx.views.RenderBlog(w, ctx.menu, "blog", articles, pages, page)
}

func (ctx *serveContext) handlePage(w http.ResponseWriter, r *http.Request) error {
	vars := mux.Vars(r)
	pageName := vars["page"]

	page, ok := ctx.pages[pageName]
	if !ok {
		return errNotFound
	}

	return ctx.views.RenderContent(w, ctx.menu, pageName, page)
}

const blogArticlesOnHomepage = 3

func (ctx *serveContext) handleHome(w http.ResponseWriter, r *http.Request) error {
	articles, _, err := ctx.getBlogData(blogArticlesOnHomepage, 0)

	if err != nil {
		return err
	}

	return ctx.views.RenderStart(w, ctx.menu, "", ctx.pages["hello"], articles)
}

func cmdServe(progname string, env *environment.Env, args []string) {
	config, err := env.Config()
	if err != nil {
		log.Fatalf("Could not load config: %s", err)
	}

	ctx, err := newServeContext(env)
	if err != nil {
		log.Fatalf("Could not create serveContext: %s", err)
	}

	r := mux.NewRouter()

	if config.StaticPath != "" {
		r.PathPrefix("/static/").Handler(http.StripPrefix("/static/", http.FileServer(http.Dir(config.StaticPath))))
	}

	r.HandleFunc("/__update", ctx.handleUpdate)
	r.HandleFunc("/blog/q/{slug}", wrapHandleFunc("article-quicklink", ctx.handleArticleQuicklink))
	r.HandleFunc("/blog/{year:[0-9]+}/{month:[0-9]+}/{day:[0-9]+}/{slug}", wrapHandleFunc("article", ctx.handleArticle))
	r.HandleFunc("/blog/{year:[0-9]+}/{month:[0-9]+}/{day:[0-9]+}", wrapHandleFunc("archiveDay", ctx.handleArchiveDay))
	r.HandleFunc("/blog/{year:[0-9]+}/{month:[0-9]+}", wrapHandleFunc("archiveMonth", ctx.handleArchiveMonth))
	r.HandleFunc("/blog/{year:[0-9]+}", wrapHandleFunc("archiveYear", ctx.handleArchiveYear))
	r.HandleFunc("/blog/archive", wrapHandleFunc("archive", ctx.handleArchive))
	r.HandleFunc("/blog/tags/{tag}", wrapHandleFunc("tag", ctx.handleTag))
	r.HandleFunc("/blog/tags", wrapHandleFunc("tags", ctx.handleTags))
	r.HandleFunc("/blog/search", wrapHandleFunc("search", ctx.handleSearch))
	r.HandleFunc("/blog/feed.xml", wrapHandleFunc("feed", ctx.handleFeed))
	r.HandleFunc("/blog", wrapHandleFunc("blog", ctx.handleBlog))
	r.HandleFunc("/{page}", wrapHandleFunc("page", ctx.handlePage))
	r.HandleFunc("/", wrapHandleFunc("home", ctx.handleHome))

	if err := http.ListenAndServe(config.HttpLaddr, r); err != nil {
		log.Fatalln(err)
	}
}
