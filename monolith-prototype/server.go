package main

import (
	"database/sql"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"strconv"

	"github.com/a-h/templ"
	bolt "go.etcd.io/bbolt"
	_ "modernc.org/sqlite"
)

func NewServer(usersDB string) *Server {
	db, err := sql.Open("sqlite", usersDB)
	if err != nil {
		panic(err.Error())
	}

	// Make tables if they don't exist
	_, err = db.Exec(`create table if not exists users (
		id integer primary key autoincrement,
		username text not null,
		password text not null
	);
	create table if not exists user_collections (
		collection_table_id integer not null,
		name text not null,
		user_id integer not null
	);
	`)
	if err != nil {
		panic(err.Error())
	}

	kv, err := bolt.Open("kv_bolt.db", 0600, nil)
	if err != nil {
		panic(err.Error())
	}

	kv.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte("userSessions"))
		return err
	})

	mux := http.NewServeMux()

	return &Server{
		Mux: mux,
		db:  db,
		kv:  kv,

		imagesFS: os.DirFS("images"),
	}
}

type Server struct {
	Mux *http.ServeMux
	db  *sql.DB
	kv  *bolt.DB

	imagesFS fs.FS
}

func (s *Server) ListenAndServe(port string) {

	s.Mux.Handle("/", templ.Handler(Homepage()))
	s.Mux.HandleFunc("/login", s.Login)
	s.Mux.HandleFunc("/signup", s.Signup)
	s.Mux.Handle("/images/", http.FileServerFS(s.imagesFS))
	s.Mux.HandleFunc("/dashboard/{id}", s.Dashboard)
	s.Mux.HandleFunc("/new_collection", s.NewCollection)
	s.Mux.HandleFunc("/add_item/{id}", s.AddItem)

	http.ListenAndServe(port, s.Mux)
}

func (s *Server) Close() {
	s.db.Close()
	s.kv.Close()
}

type UserItem struct {
	Name      string `json:"name"`
	Metadata  string `json:"metadata"`
	ImageRefs string `json:"image_refs"`
}

type Collection struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (s *Server) Dashboard(w http.ResponseWriter, r *http.Request) {
	activeUser, username := s.GetUserFromRequest(r)
	if !activeUser {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	row := s.db.QueryRow(`select id from users where username = ?`, username)
	userID := -1
	if row.Scan(&userID) != nil {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}

	showMetadata := r.URL.Query().Get("show") == "true"

	dbID, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || dbID < 0 {
		http.Error(w, "invalid collection id", http.StatusBadRequest)
		return
	}

	collectionName := ""
	user_collections := []Collection{}
	rows, err := s.db.Query(`select collection_table_id, name from user_collections where user_id = ?`, userID)
	for rows.Next() {
		row := Collection{}
		rowID := 0
		err := rows.Scan(&rowID, &row.Name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if rowID == dbID {
			collectionName = row.Name
		}
		row.ID = fmt.Sprintf("%d", rowID)
		user_collections = append(user_collections, row)
	}

	rows, err = s.db.Query(fmt.Sprintf(`select name, metadata, image_refs from user_%d_collection_%d`, userID, dbID))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	userItems := make([]UserItem, 0)
	for rows.Next() {
		row := UserItem{}
		err := rows.Scan(&row.Name, &row.Metadata, &row.ImageRefs)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		userItems = append(userItems, row)
	}

	viewMode := r.URL.Query().Get("view")
	if viewMode == "pokedex" {
		templ.Handler(Pokedex(collectionName, fmt.Sprintf("%d", dbID), user_collections, userItems, showMetadata)).ServeHTTP(w, r)
		return
	}

	templ.Handler(Dashboard(collectionName, fmt.Sprintf("%d", dbID), user_collections, userItems, showMetadata)).ServeHTTP(w, r)

}

func (s *Server) NewCollection(w http.ResponseWriter, r *http.Request) {

	activeUser, username := s.GetUserFromRequest(r)
	if !activeUser {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if r.Method == "GET" {
		templ.Handler(NewCollection()).ServeHTTP(w, r)
		return
	}

	collectionName := r.FormValue("name")

	// 1. get user id
	row := s.db.QueryRow(`select id from users where username = ?`, username)
	var id int
	if err := row.Scan(&id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 2. get user max collection id
	max_collection_id := -1
	row = s.db.QueryRow(`select max(collection_table_id) from user_collections where user_id = ?`, id)
	if err := row.Scan(&max_collection_id); err != nil {
		max_collection_id = -1
	}
	max_collection_id++

	// 3. make table using collection id
	_, err := s.db.Exec(fmt.Sprintf(`CREATE TABLE user_%d_collection_%d (id integer primary key autoincrement, name text, metadata text, image_refs text)`, id, max_collection_id))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 4. connect collection id to user id
	_, err = s.db.Exec(`insert into user_collections (collection_table_id, name, user_id) values (?, ?, ?)`, max_collection_id, collectionName, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/dashboard/%d", max_collection_id), http.StatusSeeOther)
}

func (s *Server) AddItem(w http.ResponseWriter, r *http.Request) {
	activeUser, username := s.GetUserFromRequest(r)
	if !activeUser {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if r.Method == "GET" {
		templ.Handler(AddItem()).ServeHTTP(w, r)
		return
	}

	userID := -1
	row := s.db.QueryRow(`select id from users where username = ?`, username)
	if row.Scan(&userID) != nil {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}

	name := r.FormValue("name")
	meta := r.FormValue("metadata")

	collectionID, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || collectionID < 0 {
		http.Error(w, "invalid collection id", http.StatusBadRequest)
		return
	}

	_, err = s.db.Exec(fmt.Sprintf(`insert into user_%d_collection_%d (name, metadata, image_refs) values (?, ?, ?)`, userID, collectionID), name, meta, "[]")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/dashboard/%d", collectionID), http.StatusSeeOther)
}
