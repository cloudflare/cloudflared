package gosqljson

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// QueryDbToArrayJSON - run the sql and return a a JSON string of array
func QueryDbToArrayJSON(db *sql.DB, theCase string, sqlStatement string, sqlParams ...interface{}) (string, error) {
	headers, data, err := QueryDbToArray(db, theCase, sqlStatement, sqlParams...)
	result := map[string]interface{}{
		"headers": headers,
		"data":    data,
	}
	jsonString, err := json.Marshal(result)
	return string(jsonString), err
}

// QueryDbToMapJSON - run the sql and return a JSON string of array of objects.
func QueryDbToMapJSON(db *sql.DB, theCase string, sqlStatement string, sqlParams ...interface{}) (string, error) {
	data, err := QueryDbToMap(db, theCase, sqlStatement, sqlParams...)
	jsonString, err := json.Marshal(data)
	return string(jsonString), err
}

// QueryDbToArray - headers, data, error
func QueryDbToArray(db *sql.DB, theCase string, sqlStatement string, sqlParams ...interface{}) ([]string, [][]string, error) {
	defer func() {
		if err := recover(); err != nil {
			fmt.Println(err)
		}
	}()

	data := [][]string{}
	headers := []string{}
	rows, err := db.Query(sqlStatement, sqlParams...)
	if err != nil {
		fmt.Println("Error executing: ", sqlStatement)
		return headers, data, err
	}
	cols, _ := rows.Columns()
	if theCase == "lower" {
		colsLower := make([]string, len(cols))
		for i, v := range cols {
			colsLower[i] = strings.ToLower(v)
		}
		headers = colsLower
	} else if theCase == "upper" {
		colsUpper := make([]string, len(cols))
		for i, v := range cols {
			colsUpper[i] = strings.ToUpper(v)
		}
		headers = colsUpper
	} else if theCase == "camel" {
		colsCamel := make([]string, len(cols))
		for i, v := range cols {
			colsCamel[i] = toCamel(v)
		}
		headers = colsCamel
	} else {
		headers = cols
	}

	rawResult := make([][]byte, len(cols))

	dest := make([]interface{}, len(cols)) // A temporary interface{} slice
	for i := range rawResult {
		dest[i] = &rawResult[i] // Put pointers to each string in the interface slice
	}

	for rows.Next() {
		result := make([]string, len(cols))
		rows.Scan(dest...)
		for i, raw := range rawResult {
			if raw == nil {
				result[i] = ""
			} else {
				result[i] = string(raw)
			}
		}
		data = append(data, result)
	}
	return headers, data, nil
}

// QueryTxToArray - headers, data, error
func QueryTxToArray(tx *sql.Tx, theCase string, sqlStatement string, sqlParams ...interface{}) ([]string, [][]string, error) {
	defer func() {
		if err := recover(); err != nil {
			fmt.Println(err)
		}
	}()

	data := [][]string{}
	headers := []string{}
	rows, err := tx.Query(sqlStatement, sqlParams...)
	if err != nil {
		fmt.Println("Error executing: ", sqlStatement)
		return headers, data, err
	}
	cols, _ := rows.Columns()
	if theCase == "lower" {
		colsLower := make([]string, len(cols))
		for i, v := range cols {
			colsLower[i] = strings.ToLower(v)
		}
		headers = colsLower
	} else if theCase == "upper" {
		colsUpper := make([]string, len(cols))
		for i, v := range cols {
			colsUpper[i] = strings.ToUpper(v)
		}
		headers = colsUpper
	} else if theCase == "camel" {
		colsCamel := make([]string, len(cols))
		for i, v := range cols {
			colsCamel[i] = toCamel(v)
		}
		headers = colsCamel
	} else {
		headers = cols
	}

	rawResult := make([][]byte, len(cols))

	dest := make([]interface{}, len(cols)) // A temporary interface{} slice
	for i := range rawResult {
		dest[i] = &rawResult[i] // Put pointers to each string in the interface slice
	}

	for rows.Next() {
		result := make([]string, len(cols))
		rows.Scan(dest...)
		for i, raw := range rawResult {
			if raw == nil {
				result[i] = ""
			} else {
				result[i] = string(raw)
			}
		}
		data = append(data, result)
	}
	return headers, data, nil
}

// QueryDbToMap - run sql and return an array of maps
func QueryDbToMap(db *sql.DB, theCase string, sqlStatement string, sqlParams ...interface{}) ([]map[string]string, error) {
	defer func() {
		if err := recover(); err != nil {
			fmt.Println(err)
		}
	}()

	results := []map[string]string{}
	rows, err := db.Query(sqlStatement, sqlParams...)
	if err != nil {
		fmt.Println("Error executing: ", sqlStatement)
		return results, err
	}
	cols, _ := rows.Columns()
	colsLower := make([]string, len(cols))
	colsCamel := make([]string, len(cols))

	if theCase == "lower" {
		for i, v := range cols {
			colsLower[i] = strings.ToLower(v)
		}
	} else if theCase == "upper" {
		for i, v := range cols {
			cols[i] = strings.ToUpper(v)
		}
	} else if theCase == "camel" {
		for i, v := range cols {
			colsCamel[i] = toCamel(v)
		}
	}

	rawResult := make([][]byte, len(cols))

	dest := make([]interface{}, len(cols)) // A temporary interface{} slice
	for i := range rawResult {
		dest[i] = &rawResult[i] // Put pointers to each string in the interface slice
	}

	for rows.Next() {
		result := make(map[string]string, len(cols))
		rows.Scan(dest...)
		for i, raw := range rawResult {
			if raw == nil {
				if theCase == "lower" {
					result[colsLower[i]] = ""
				} else if theCase == "upper" {
					result[cols[i]] = ""
				} else if theCase == "camel" {
					result[colsCamel[i]] = ""
				} else {
					result[cols[i]] = ""
				}
			} else {
				if theCase == "lower" {
					result[colsLower[i]] = string(raw)
				} else if theCase == "upper" {
					result[cols[i]] = string(raw)
				} else if theCase == "camel" {
					result[colsCamel[i]] = string(raw)
				} else {
					result[cols[i]] = string(raw)
				}
			}
		}
		results = append(results, result)
	}
	return results, nil
}

// QueryTxToMap - run sql and return an array of maps
func QueryTxToMap(tx *sql.Tx, theCase string, sqlStatement string, sqlParams ...interface{}) ([]map[string]string, error) {
	defer func() {
		if err := recover(); err != nil {
			fmt.Println(err)
		}
	}()

	results := []map[string]string{}
	rows, err := tx.Query(sqlStatement, sqlParams...)
	if err != nil {
		fmt.Println("Error executing: ", sqlStatement)
		return results, err
	}
	cols, _ := rows.Columns()
	colsLower := make([]string, len(cols))
	colsCamel := make([]string, len(cols))

	if theCase == "lower" {
		for i, v := range cols {
			colsLower[i] = strings.ToLower(v)
		}
	} else if theCase == "upper" {
		for i, v := range cols {
			cols[i] = strings.ToUpper(v)
		}
	} else if theCase == "camel" {
		for i, v := range cols {
			colsCamel[i] = toCamel(v)
		}
	}

	rawResult := make([][]byte, len(cols))

	dest := make([]interface{}, len(cols)) // A temporary interface{} slice
	for i := range rawResult {
		dest[i] = &rawResult[i] // Put pointers to each string in the interface slice
	}

	for rows.Next() {
		result := make(map[string]string, len(cols))
		rows.Scan(dest...)
		for i, raw := range rawResult {
			if raw == nil {
				if theCase == "lower" {
					result[colsLower[i]] = ""
				} else if theCase == "upper" {
					result[cols[i]] = ""
				} else if theCase == "camel" {
					result[colsCamel[i]] = ""
				} else {
					result[cols[i]] = ""
				}
			} else {
				if theCase == "lower" {
					result[colsLower[i]] = string(raw)
				} else if theCase == "upper" {
					result[cols[i]] = string(raw)
				} else if theCase == "camel" {
					result[colsCamel[i]] = string(raw)
				} else {
					result[cols[i]] = string(raw)
				}
			}
		}
		results = append(results, result)
	}
	return results, nil
}

// ExecDb - run the sql and returns rows affected.
func ExecDb(db *sql.DB, sqlStatement string, sqlParams ...interface{}) (int64, error) {
	defer func() {
		if err := recover(); err != nil {
			fmt.Println(err)
		}
	}()
	result, err := db.Exec(sqlStatement, sqlParams...)
	if err != nil {
		fmt.Println("Error executing: ", sqlStatement)
		fmt.Println(err)
		return 0, err
	}
	return result.RowsAffected()
}

// ExecTx - run the sql and returns rows affected.
func ExecTx(tx *sql.Tx, sqlStatement string, sqlParams ...interface{}) (int64, error) {
	defer func() {
		if err := recover(); err != nil {
			fmt.Println(err)
		}
	}()

	result, err := tx.Exec(sqlStatement, sqlParams...)
	if err != nil {
		fmt.Println("Error executing: ", sqlStatement)
		fmt.Println(err)
		return 0, err
	}
	return result.RowsAffected()
}

func toCamel(s string) (ret string) {
	s = strings.ToLower(s)
	a := strings.Split(s, "_")
	for i, v := range a {
		if i == 0 {
			ret += v
		} else {
			f := strings.ToUpper(string(v[0]))
			n := string(v[1:])
			ret += fmt.Sprint(f, n)
		}
	}
	return
}
