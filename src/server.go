package kvass

import (
	"bytes"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
)

// TODO: setup real token
var key = []byte("VERY_SECRET_TODO")

func isAuthorized(r *http.Request) bool {
	token := r.Header.Get("x-auth-token")
	log.Printf("%v", token)
	if 1 == subtle.ConstantTimeCompare(key, []byte(token)) {
		return true
	}

	return false
}

func RunServer(p *SqlitePersistance, bind string) {
	logger := log.New(os.Stdout, "", log.LstdFlags)

	http.HandleFunc("/push", p.pushHandler)
	http.HandleFunc("/pull", p.pullHandler)
	http.HandleFunc("/get", p.getHandler)

	logger.Printf("Server started and listening on %v\n", bind)
	panic(http.ListenAndServe(bind, nil))
}

func (p *SqlitePersistance) pullHandler(w http.ResponseWriter, r *http.Request) {
	if !isAuthorized(r) {
		http.Error(w, "incorrect auth token", 400)
		return
	}

	payload, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	ex := make([]KvEntry, 0)

	entries := unmarshal(payload, &ex)
	for _, e := range entries {
		p.UpdateOn(e)
	}

}

// TODO: is this really pushHandler?
func (p *SqlitePersistance) pushHandler(w http.ResponseWriter, r *http.Request) {
	if !isAuthorized(r) {
		http.Error(w, "incorrect auth token", 400)
		return
	}

	payload, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	updateRequest := UpdateRequest{}
	err = json.Unmarshal(payload, &updateRequest)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	updates, err := p.GetUpdates(updateRequest)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	response_payload, err := json.MarshalIndent(updates, "", " ")
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	w.Write(response_payload)
}

func (p *SqlitePersistance) getHandler(w http.ResponseWriter, r *http.Request) {
	file := r.URL.Query().Get("q")

	if file == "" {
		http.Error(w, "Please specify file", 400)
		return
	}

	row := p.db.QueryRow("select key from entries where urltoken = ?;", file)
	var key string
	err := row.Scan(&key)
	if err == sql.ErrNoRows {
		http.Error(w, "Unknown File", 404)
		return
	}

	entry, err := p.GetEntry(key)
	if err != nil {
		http.Error(w, err.Error(), 500)
	}

	if entry == nil {
		http.Error(w, "Aaaaand it's gone", 419) // entry was deleted since we got the key
		return
	}

	if strings.HasSuffix(key, ".html") {
		r.Header.Add("Content-Type", "application/html")
	}

	io.Copy(w, bytes.NewBuffer(entry.Value))
}
