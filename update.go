package main

import (
	"database/sql"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	"code.laria.me/laria.me/article"
	"code.laria.me/laria.me/config"
	"code.laria.me/laria.me/environment"
)

func allArticlesFromDir(dir string) ([]article.Article, error) {
	f, err := os.Open(dir)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	infos, err := f.Readdir(-1)
	if err != nil {
		return nil, err
	}

	articles := make([]article.Article, 0)

	for _, info := range infos {
		if info.IsDir() {
			continue
		}

		fullname := filepath.Join(dir, info.Name())
		a, err := article.LoadArticle(fullname)
		if err != nil {
			return nil, err
		}

		articles = append(articles, a)
	}

	return articles, nil
}

func updateArticles(conf *config.Config, db *sql.DB) {
	articles := []article.Article{}
	for _, dir := range conf.ArticleDirs {
		dirArticles, err := allArticlesFromDir(dir)
		if err != nil {
			log.Fatalf("allArticlesFromDir(%s): %s", dir, err)
		}

		articles = append(articles, dirArticles...)
	}

	slugs := make([]string, 0, len(articles))
	for _, article := range articles {
		if _, err := article.SaveToDb(db); err != nil {
			log.Fatalf("SaveToDb: %s", err)
		}

		slugs = append(slugs, article.Slug)
	}

	if err := article.DeleteArticlesFromDbExcept(db, slugs); err != nil {
		log.Fatalf("DeleteArticlesFromDbExcept: %s", err)
	}
}

func cmdUpdate(progname string, env *environment.Env, args []string) {
	conf, err := env.Config()
	if err != nil {
		log.Fatalf("env.Config() failed: %s", err)
	}

	db, err := env.DB()
	if err != nil {
		log.Fatalf("env.Config() failed: %s", err)
	}

	updateArticles(conf, db)

	resp, err := http.PostForm(conf.UpdateUrl, url.Values{"secret": {conf.Secret}})
	if err != nil {
		log.Fatalf("triggering server update failed: %s", err)
	}

	if resp.StatusCode != 200 {
		log.Fatalf("server update unexpectedly responded with %d %s", resp.StatusCode, resp.Status)
	}
}
