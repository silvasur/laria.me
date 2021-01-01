package dbutils

import (
	"database/sql"
	"fmt"
	"strings"
)

func TxCommitIfOk(tx *sql.Tx, err error) error {
	if err != nil {
		if rollbackErr := tx.Rollback(); rollbackErr != nil {
			return fmt.Errorf("Failed rolling back transaction with error \"%s\" while handling error %w", rollbackErr, err)
		}

		return err
	} else {
		if err = tx.Commit(); err != nil {
			return err
		}

		return nil
	}
}

func BuildInIdsSqlAndArgs(field string, ids []int) (string, []interface{}) {
	if len(ids) == 0 {
		return "0", []interface{}{}
	}

	sb := new(strings.Builder)
	sb.WriteString(field)
	sb.WriteString(" IN (?")

	for i := 1; i < len(ids); i++ {
		sb.WriteString(",?")
	}
	sb.WriteRune(')')

	args := make([]interface{}, 0, len(ids))
	for _, id := range ids {
		args = append(args, interface{}(id))
	}

	return sb.String(), args
}

// GetIndexedValues
// func GetIndexedValues(db *sql.DB, m interface{}, query, args ...interface{}) error {
// 	mv := reflect.ValueOf(m)
// 	mt := mv.Type()
// 	if mt.Kind() != reflect.Map {
// 		panic("GetIndexValue needs a map as argument m")
// 	}
// 	if mt.

// 	rows, err := db.Query(query, args...)
// 	if err != nil {
// 		return err
// 	}
// }
