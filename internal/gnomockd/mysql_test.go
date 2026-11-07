package gnomockd_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"testing"

	"github.com/orlangure/gnomock"
	"github.com/orlangure/gnomock/internal/gnomockd"
	_ "github.com/orlangure/gnomock/preset/mysql"
	"github.com/stretchr/testify/require"
)

func TestMySQL(t *testing.T) {
	t.Parallel()

	if runtime.GOARCH == "arm64" {
		// Known gap (PLAN.md): testdata/mysql.json asks for version "8", and
		// the arm64 mysql preset maps it to mysql/mysql-server:8, which does not
		// exist. Phase 6 drops the per-arch image split; delete this skip then.
		t.Skip("mysql/mysql-server:8 does not exist for arm64")
	}

	h := gnomockd.Handler()
	bs, err := os.ReadFile("./testdata/mysql.json")
	require.NoError(t, err)

	buf := bytes.NewBuffer(bs)
	w, r := httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/start/mysql", buf)
	h.ServeHTTP(w, r)

	res := w.Result()

	defer func() { require.NoError(t, res.Body.Close()) }()

	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)

	require.Equalf(t, http.StatusOK, res.StatusCode, string(body))

	var c *gnomock.Container

	err = json.Unmarshal(body, &c)
	require.NoError(t, err)

	addr := c.DefaultAddress()
	connStr := fmt.Sprintf(
		"%s:%s@tcp(%s)/%s",
		"gnomock", "foobar", addr, "gnomockd_db",
	)

	db, err := sql.Open("mysql", connStr)
	require.NoError(t, err)

	row := db.QueryRow(`select count(distinct ip_address) from customers`)
	count := 0
	require.NoError(t, row.Scan(&count))
	require.Equal(t, 1000, count)

	row = db.QueryRow(`select a from tbl`)
	value := 0
	require.NoError(t, row.Scan(&value))
	require.Equal(t, 42, value)

	bs, err = json.Marshal(c)
	require.NoError(t, err)

	buf = bytes.NewBuffer(bs)
	w, r = httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/stop", buf)
	h.ServeHTTP(w, r)

	res = w.Result()
	require.Equal(t, http.StatusOK, res.StatusCode)
}
