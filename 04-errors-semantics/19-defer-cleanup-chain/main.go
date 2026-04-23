package main

import (
	"context"
	"errors"
	"io"
)

// File is a writable, closeable file handle.
type File interface {
	io.WriteCloser
}

// Rows is a database result set that can be iterated and closed.
type Rows interface {
	Next() bool
	Scan(dest ...any) error
	Close() error
}

// Tx is a database transaction.
type Tx interface {
	QueryRows(ctx context.Context) (Rows, error)
	Commit() error
	Rollback() error
}

// DB is a database connection.
type DB interface {
	BeginTx(ctx context.Context) (Tx, error)
	Close() error
}

// Deps holds injectable dependencies for BackupDatabase.
type Deps struct {
	OpenFile  func(filename string) (File, error)
	ConnectDB func(ctx context.Context, dbURL string) (DB, error)
}

func BackupDatabase(ctx context.Context, dbURL, filename string, deps Deps) (err error) {
	file, err := deps.OpenFile(filename)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, file.Close())
	}()

	db, err := deps.ConnectDB(ctx, dbURL)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, db.Close())
	}()

	tx, err := db.BeginTx(ctx)
	if err != nil {
		return
	}
	committed := false
	defer func() {
		if !committed {
			err = errors.Join(err, tx.Rollback())
		}
	}()

	rows, err := tx.QueryRows(ctx)
	if err != nil {
		return
	}
	defer func() {
		err = errors.Join(err, rows.Close())
	}()

	var id int
	var data string
	for rows.Next() {
		if err = rows.Scan(&id, &data); err != nil {
			return
		}
		if _, err = file.Write([]byte(data + "\n")); err != nil {
			return
		}
	}

	if err = tx.Commit(); err != nil {
		return
	}
	committed = true
	return
}

func main() {}
