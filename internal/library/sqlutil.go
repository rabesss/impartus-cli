package library

import (
	"database/sql"
	"errors"
	"os"
)

func rollbackUnlessCommitted(transaction *sql.Tx, committed *bool) {
	if *committed {
		return
	}
	if err := transaction.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		return
	}
}

func closeRows(rows *sql.Rows) {
	if err := rows.Close(); err != nil {
		return
	}
}

func closeConnection(connection *sql.Conn) {
	if err := connection.Close(); err != nil {
		return
	}
}

func closeFile(file *os.File) {
	if err := file.Close(); err != nil {
		return
	}
}
