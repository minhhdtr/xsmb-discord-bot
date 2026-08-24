package bot_test

import "database/sql"

func openDB(dsn string) (*sql.DB, error) { return sql.Open("postgres", dsn) }
