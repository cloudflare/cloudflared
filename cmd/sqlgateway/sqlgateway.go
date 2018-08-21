package sqlgateway

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"strings"
	"time"

	_ "github.com/lib/pq"
	cli "gopkg.in/urfave/cli.v2"

	"github.com/elgs/gosqljson"

	"github.com/gorilla/mux"
	"github.com/sirupsen/logrus"
)

type Message struct {
	Connection Connection    `json:"connection"`
	Command    string        `json:"command"`
	Params     []interface{} `json:"params"`
}

type Connection struct {
	SSLMode string `json:"sslmode"`
	Token   string `json:"token"`
}

type Response struct {
	Columns []string   `json:"columns"`
	Rows    [][]string `json:"rows"`
	Error   string     `json:"error"`
}

type Proxy struct {
	Context  *cli.Context
	Router   *mux.Router
	Token    string
	User     string
	Password string
	Driver   string
	Database string
	Logger   *logrus.Logger
}

func StartProxy(c *cli.Context, logger *logrus.Logger, password string) error {
	proxy := NewProxy(c, logger, password)

	logger.Infof("Starting SQL Gateway Proxy on port %s", strings.Split(c.String("url"), ":")[1])

	err := http.ListenAndServe(":"+strings.Split(c.String("url"), ":")[1], proxy.Router)
	if err != nil {
		return err
	}

	return nil
}

func randID(n int, c *cli.Context) string {
	charBytes := []byte("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890")
	b := make([]byte, n)
	for i := range b {
		b[i] = charBytes[rand.Intn(len(charBytes))]
	}
	return fmt.Sprintf("%s&%s", c.String("hostname"), b)
}

// db://user@dbname
func parseInfo(input string) (string, string, string) {
	p1 := strings.Split(input, "://")
	p2 := strings.Split(p1[1], "@")
	return p1[0], p2[0], p2[1]
}

func NewProxy(c *cli.Context, logger *logrus.Logger, pass string) *Proxy {
	rand.Seed(time.Now().UnixNano())
	driver, user, dbname := parseInfo(c.String("address"))
	proxy := Proxy{
		Context:  c,
		Router:   mux.NewRouter(),
		Token:    randID(64, c),
		Logger:   logger,
		User:     user,
		Password: pass,
		Database: dbname,
		Driver:   driver,
	}

	logger.Info(fmt.Sprintf(`

	--------------------
	SQL Gateway Proxy
	Token: %s
	--------------------

	`, proxy.Token))

	proxy.Router.HandleFunc("/", proxy.proxyRequest).Methods("POST")
	return &proxy
}

func (proxy *Proxy) proxyRequest(rw http.ResponseWriter, req *http.Request) {
	var message Message
	response := Response{}

	err := json.NewDecoder(req.Body).Decode(&message)
	if err != nil {
		proxy.Logger.Error(err)
		http.Error(rw, fmt.Sprintf("400 - %s", err.Error()), http.StatusBadRequest)
		return
	}

	if message.Connection.Token != proxy.Token {
		proxy.Logger.Error("Invalid token")
		http.Error(rw, "400 - Invalid token", http.StatusBadRequest)
		return
	}

	connStr := fmt.Sprintf("user=%s password=%s dbname=%s sslmode=%s", proxy.User, proxy.Password, proxy.Database, message.Connection.SSLMode)

	db, err := sql.Open(proxy.Driver, connStr)
	defer db.Close()

	if err != nil {
		proxy.Logger.Error(err)
		http.Error(rw, fmt.Sprintf("400 - %s", err.Error()), http.StatusBadRequest)
		return

	} else {
		proxy.Logger.Info("Forwarding SQL: ", message.Command)
		rw.Header().Set("Content-Type", "application/json")

		headers, data, err := gosqljson.QueryDbToArray(db, "lower", message.Command, message.Params...)

		if err != nil {
			proxy.Logger.Error(err)
			http.Error(rw, fmt.Sprintf("400 - %s", err.Error()), http.StatusBadRequest)
			return

		} else {
			response = Response{headers, data, ""}
		}
	}
	json.NewEncoder(rw).Encode(response)
}
