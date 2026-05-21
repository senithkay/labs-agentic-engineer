package services

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	_ "github.com/go-sql-driver/mysql"
)

type mysqlBackend struct {
	rootURL string
	host    string
	port    int
}

func newMySQLBackend(rootURL, host string, port int) *mysqlBackend {
	return &mysqlBackend{rootURL: rootURL, host: host, port: port}
}

func (b *mysqlBackend) ping(ctx context.Context) error {
	db, err := sql.Open("mysql", b.rootURL)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer db.Close()
	return db.PingContext(ctx)
}

// testCredentials verifies that a specific user/password can connect to the named database.
func (b *mysqlBackend) testCredentials(ctx context.Context, host string, port int, dbName, username, password string) error {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true", username, password, host, port, dbName)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer db.Close()
	return db.PingContext(ctx)
}

func (b *mysqlBackend) createDatabase(ctx context.Context, name string) (*DatabaseCredentials, error) {
	dbName := sanitizeIdentifier(name)

	suffix, err := randString(6)
	if err != nil {
		return nil, fmt.Errorf("generate suffix: %w", err)
	}
	username := "u_" + suffix

	password, err := randString(20)
	if err != nil {
		return nil, fmt.Errorf("generate password: %w", err)
	}

	db, err := sql.Open("mysql", b.rootURL)
	if err != nil {
		return nil, fmt.Errorf("connect root: %w", err)
	}
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping root: %w", err)
	}

	if _, err := db.ExecContext(ctx, fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s`", dbName)); err != nil {
		return nil, fmt.Errorf("create database: %w", err)
	}

	if _, err := db.ExecContext(ctx, fmt.Sprintf(
		"CREATE USER IF NOT EXISTS '%s'@'%%' IDENTIFIED BY '%s'",
		username, password,
	)); err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}

	if _, err := db.ExecContext(ctx, fmt.Sprintf(
		"GRANT ALL PRIVILEGES ON `%s`.* TO '%s'@'%%'",
		dbName, username,
	)); err != nil {
		return nil, fmt.Errorf("grant privileges: %w", err)
	}

	if _, err := db.ExecContext(ctx, "FLUSH PRIVILEGES"); err != nil {
		return nil, fmt.Errorf("flush privileges: %w", err)
	}

	slog.InfoContext(ctx, "mysql database created", "database", dbName)
	return &DatabaseCredentials{
		DBType:   string(DBTypeMySQL),
		Host:     b.host,
		Port:     b.port,
		Database: dbName,
		Username: username,
		Password: password,
	}, nil
}
