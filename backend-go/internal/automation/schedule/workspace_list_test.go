package schedule

import (
	"context"
	"github.com/DATA-DOG/go-sqlmock"
	"testing"
)

func TestListFiltersApplicationBeforePaging(t *testing.T) {
	for _, appID := range []int64{0, 7} {
		conn, m, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		var app any
		if appID > 0 {
			app = appID
		}
		m.ExpectQuery(`(?s)ListSchedulesByOwner.*s.application_id =.*ORDER BY s.id DESC.*LIMIT`).WithArgs(uint64(12), app, app, "all", "all", "all", nil, "%", uint64(0), uint64(0), int32(21)).WillReturnRows(sqlmock.NewRows([]string{"id"}))
		_, err = NewService(conn, nil, nil).List(context.Background(), 12, ListFilter{ApplicationID: appID, Limit: 21})
		if err != nil {
			t.Fatal(err)
		}
		if err = m.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
		conn.Close()
	}
}
