// Package mysqllog silences the global logger of the MySQL driver, which the
// mysql and mariadb presets share.
package mysqllog

import (
	"io"
	"log"
	"sync"

	mysqldriver "github.com/go-sql-driver/mysql"
)

var once sync.Once

// Discard replaces the MySQL driver's logger with one that discards
// everything. The driver keeps its logger in a package-level variable without
// synchronization, so every preset must go through this one sync.Once:
// separate guards in the mysql and mariadb presets raced when both presets
// were configured concurrently (gnomockd serving /start/mysql and
// /start/mariadb at once).
func Discard() {
	once.Do(func() {
		// err is always nil for a non-nil logger
		_ = mysqldriver.SetLogger(log.New(io.Discard, "", -1))
	})
}
