package main

import (
	"context"
	"errors"
)

// Compile-time interface checks.
var (
	_ File = (*mockFile)(nil)
	_ Rows = (*mockRows)(nil)
	_ Tx   = (*mockTx)(nil)
	_ DB   = (*mockDB)(nil)
)

// Sentinel errors for table-driven tests.
var (
	errOpenFile  = errors.New("open file error")
	errConnect   = errors.New("connect error")
	errBeginTx   = errors.New("begin tx error")
	errQuery     = errors.New("query error")
	errScan      = errors.New("scan error")
	errWrite     = errors.New("write error")
	errCommit    = errors.New("commit error")
	errRollback  = errors.New("rollback error")
	errFileClose  = errors.New("file close error")
	errDBClose    = errors.New("db close error")
	errRowsClose  = errors.New("rows close error")
)

type mockFile struct {
	writeErr error
	closeErr error
	closed   bool
}

func (f *mockFile) Write(p []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	return len(p), nil
}

func (f *mockFile) Close() error {
	f.closed = true
	return f.closeErr
}

type mockRows struct {
	data         []string
	pos          int
	scanErr      error
	rowsCloseErr error
}

func (r *mockRows) Next() bool { return r.pos < len(r.data) }

func (r *mockRows) Scan(dest ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	if len(dest) > 0 {
		if s, ok := dest[0].(*string); ok {
			*s = r.data[r.pos]
		}
	}
	r.pos++
	return nil
}

func (r *mockRows) Close() error { return r.rowsCloseErr }

type mockTx struct {
	rows           *mockRows
	queryErr       error
	commitErr      error
	rollbackErr    error
	commitCalled   bool
	rollbackCalled bool
}

func (t *mockTx) QueryRows(ctx context.Context) (Rows, error) {
	if t.queryErr != nil {
		return nil, t.queryErr
	}
	return t.rows, nil
}

func (t *mockTx) Commit() error {
	t.commitCalled = true
	return t.commitErr
}

func (t *mockTx) Rollback() error {
	t.rollbackCalled = true
	return t.rollbackErr
}

type mockDB struct {
	tx       *mockTx
	beginErr error
	closeErr error
	closed   bool
}

func (d *mockDB) BeginTx(ctx context.Context) (Tx, error) {
	if d.beginErr != nil {
		return nil, d.beginErr
	}
	return d.tx, nil
}

func (d *mockDB) Close() error {
	d.closed = true
	return d.closeErr
}
