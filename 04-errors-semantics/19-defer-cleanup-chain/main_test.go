package main

import (
	"context"
	"errors"
	"testing"
)

func TestBackupDatabase(t *testing.T) {
	tests := []struct {
		name string

		// Configured errors per step
		openFileErr  error
		connectErr   error
		beginTxErr   error
		queryErr     error
		scanErr      error
		writeErr     error
		commitErr    error
		rollbackErr  error
		fileCloseErr error
		dbCloseErr   error
		rowsCloseErr error
		rows         []string

		// Expected outcomes
		wantErr         bool
		wantErrContains []error // each must satisfy errors.Is(returned, want)
		wantFileClosed  bool
		wantDBClosed    bool
		wantCommit      bool
		wantRollback    bool
		wantNoRollback  bool // assert rollback was NOT called
	}{
		{
			name:           "success: commit called, all resources closed, no rollback",
			rows:           []string{"row1", "row2"},
			wantFileClosed: true,
			wantDBClosed:   true,
			wantCommit:     true,
		},
		{
			name:           "success: empty rows still commits",
			rows:           []string{},
			wantFileClosed: true,
			wantDBClosed:   true,
			wantCommit:     true,
		},
		{
			name:            "open file fails: immediate error, db never contacted",
			openFileErr:     errOpenFile,
			wantErr:         true,
			wantErrContains: []error{errOpenFile},
		},
		{
			name:            "connect db fails: file is closed",
			connectErr:      errConnect,
			wantErr:         true,
			wantErrContains: []error{errConnect},
			wantFileClosed:  true,
		},
		{
			name:            "begin tx fails: file and db both closed",
			beginTxErr:      errBeginTx,
			wantErr:         true,
			wantErrContains: []error{errBeginTx},
			wantFileClosed:  true,
			wantDBClosed:    true,
		},
		{
			name:            "query rows fails: tx rolled back, all resources closed",
			queryErr:        errQuery,
			wantErr:         true,
			wantErrContains: []error{errQuery},
			wantFileClosed:  true,
			wantDBClosed:    true,
			wantRollback:    true,
		},
		{
			name:            "scan fails: tx rolled back, all resources closed",
			rows:            []string{"row1"},
			scanErr:         errScan,
			wantErr:         true,
			wantErrContains: []error{errScan},
			wantFileClosed:  true,
			wantDBClosed:    true,
			wantRollback:    true,
		},
		{
			name:            "write fails: tx rolled back, all resources closed",
			rows:            []string{"row1"},
			writeErr:        errWrite,
			wantErr:         true,
			wantErrContains: []error{errWrite},
			wantFileClosed:  true,
			wantDBClosed:    true,
			wantRollback:    true,
		},
		{
			name:            "commit fails: tx rolled back, all resources closed",
			rows:            []string{"row1"},
			commitErr:       errCommit,
			wantErr:         true,
			wantErrContains: []error{errCommit},
			wantFileClosed:  true,
			wantDBClosed:    true,
			wantRollback:    true,
		},
		{
			// Requirement: "Preserve both errors" via errors.Join
			name:            "commit and file close both fail: both errors preserved",
			rows:            []string{"row1"},
			commitErr:       errCommit,
			fileCloseErr:    errFileClose,
			wantErr:         true,
			wantErrContains: []error{errCommit, errFileClose},
			wantFileClosed:  true,
			wantDBClosed:    true,
			wantRollback:    true,
		},
		{
			name:            "query fails and rollback fails: both errors preserved",
			queryErr:        errQuery,
			rollbackErr:     errRollback,
			wantErr:         true,
			wantErrContains: []error{errQuery, errRollback},
			wantFileClosed:  true,
			wantDBClosed:    true,
			wantRollback:    true,
		},
		{
			name:            "query fails and db close fails: both errors preserved",
			queryErr:        errQuery,
			dbCloseErr:      errDBClose,
			wantErr:         true,
			wantErrContains: []error{errQuery, errDBClose},
			wantFileClosed:  true,
			wantDBClosed:    true,
		},
		{
			name:            "begin tx fails and db close fails: both errors preserved",
			beginTxErr:      errBeginTx,
			dbCloseErr:      errDBClose,
			wantErr:         true,
			wantErrContains: []error{errBeginTx, errDBClose},
			wantFileClosed:  true,
			wantDBClosed:    true,
		},
		{
			// Regression: the old "err != nil" check in the tx defer was poisoned
			// by rows.Close() (LIFO), causing a spurious rollback after a successful
			// commit. The committed bool flag fixes this.
			name:            "rows close fails after commit: commit preserved, no rollback",
			rows:            []string{"row1"},
			rowsCloseErr:    errRowsClose,
			wantErr:         true,
			wantErrContains: []error{errRowsClose},
			wantFileClosed:  true,
			wantDBClosed:    true,
			wantCommit:      true,
			wantNoRollback:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			file := &mockFile{writeErr: tc.writeErr, closeErr: tc.fileCloseErr}
			tx := &mockTx{
				rows:        &mockRows{data: tc.rows, scanErr: tc.scanErr, rowsCloseErr: tc.rowsCloseErr},
				queryErr:    tc.queryErr,
				commitErr:   tc.commitErr,
				rollbackErr: tc.rollbackErr,
			}
			db := &mockDB{tx: tx, beginErr: tc.beginTxErr, closeErr: tc.dbCloseErr}

			deps := Deps{
				OpenFile: func(name string) (File, error) {
					if tc.openFileErr != nil {
						return nil, tc.openFileErr
					}
					return file, nil
				},
				ConnectDB: func(ctx context.Context, url string) (DB, error) {
					if tc.connectErr != nil {
						return nil, tc.connectErr
					}
					return db, nil
				},
			}

			err := BackupDatabase(context.Background(), "db://test", "test.bak", deps)

			if (err != nil) != tc.wantErr {
				t.Errorf("error = %v, wantErr = %v", err, tc.wantErr)
			}
			for _, want := range tc.wantErrContains {
				if !errors.Is(err, want) {
					t.Errorf("error should wrap %v; got: %v", want, err)
				}
			}
			if tc.wantFileClosed && !file.closed {
				t.Error("file should have been closed")
			}
			if tc.wantDBClosed && !db.closed {
				t.Error("DB connection should have been closed")
			}
			if tc.wantCommit && !tx.commitCalled {
				t.Error("Tx.Commit should have been called")
			}
			if tc.wantRollback && !tx.rollbackCalled {
				t.Error("Tx.Rollback should have been called")
			}
			if tc.wantNoRollback && tx.rollbackCalled {
				t.Error("Tx.Rollback must not be called")
			}
			// On clean success, rollback must NOT be called (no spurious rollbacks).
			if !tc.wantErr && tx.rollbackCalled {
				t.Error("Tx.Rollback must not be called on success")
			}
		})
	}
}

// TestBackupDatabase_NoFDLeak verifies that every run releases all resources,
// regardless of how many times BackupDatabase is called (README §Self-Correction 3).
func TestBackupDatabase_NoFDLeak(t *testing.T) {
	for i := range 1000 {
		file := &mockFile{}
		tx := &mockTx{rows: &mockRows{data: []string{"row1", "row2"}}}
		db := &mockDB{tx: tx}

		deps := Deps{
			OpenFile:  func(name string) (File, error) { return file, nil },
			ConnectDB: func(ctx context.Context, url string) (DB, error) { return db, nil },
		}

		if err := BackupDatabase(context.Background(), "db://test", "test.bak", deps); err != nil {
			t.Fatalf("iteration %d: unexpected error: %v", i, err)
		}
		if !file.closed {
			t.Fatalf("iteration %d: file not closed (FD leak)", i)
		}
		if !db.closed {
			t.Fatalf("iteration %d: DB connection not closed", i)
		}
	}
}
