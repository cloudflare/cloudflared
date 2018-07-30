gosqljson
=========

A Go SQL to JSON library.

#Installation
`go get -u github.com/elgs/gosqljson`

# Sample code
Data in the table:
```
ID	NAME

0	Alicia
1	Brian
2	Chloe
4	Bianca
5	Leo
6	Joy
7	Sam
8	Elgs
```
```go
package main

import (
	"database/sql"
	"fmt"
	"github.com/elgs/gosqljson"
	_ "github.com/go-sql-driver/mysql"
)

func main() {
	ds := "username:password@tcp(host:3306)/db"
	db, err := sql.Open("mysql", ds)

	if err != nil {
		fmt.Println("sql.Open:", err)
		return
	}

	theCase := "lower" // "lower", "upper", "camel" or the orignal case if this is anything other than these three

	// headers []string, data [][]string, error
	headers, data, _ := gosqljson.QueryDbToArray(db, theCase, "SELECT ID,NAME FROM t LIMIT ?,?", 0, 3)
	fmt.Println(headers)
	// ["id","name"]
	fmt.Println(data)
	// [["0","Alicia"],["1","Brian"],["2","Chloe"]]

	// data []map[string]string, error
	data, _ := gosqljson.QueryDbToMap(db, theCase, "SELECT ID,NAME FROM t LIMIT ?,?", 0, 3)
	fmt.Println(data)
	// [{"id":"0","name":"Alicia"},{"id":"1","name":"Brian"},{"id":"2","name":"Chloe"}]
}
```