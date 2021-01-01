// Package environment provides the Env type for commonly used data in the application.
package environment

import (
	"database/sql"

	_ "github.com/go-sql-driver/mysql"

	"code.laria.me/laria.me/config"
)

// Env provides commonly used data in the application
type Env struct {
	configPath string

	config *config.Config
	db     *sql.DB
}

func New(configPath string) *Env {
	return &Env{
		configPath: configPath,
	}
}

func (e *Env) Config() (*config.Config, error) {
	if e.config != nil {
		return e.config, nil
	}

	conf, err := config.LoadConfig(e.configPath)
	if err != nil {
		return nil, err
	}

	e.config = conf
	return conf, nil
}

func (e *Env) DB() (*sql.DB, error) {
	if e.db != nil {
		return e.db, nil
	}

	conf, err := e.Config()
	if err != nil {
		return nil, err
	}

	var db *sql.DB
	db, err = sql.Open("mysql", conf.DbDsn)
	if err != nil {
		return nil, err
	}

	e.db = db
	return db, nil
}
