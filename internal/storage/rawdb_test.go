package storage_test

import "database/sql"

func openRaw(dsn string) (*sql.DB, error) { return sql.Open("postgres", dsn) }
