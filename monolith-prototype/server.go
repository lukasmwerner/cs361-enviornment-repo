package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httputil"
	"net/url"
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

	s := &Server{
		Mux: mux,
		db:  db,
		kv:  kv,

		imagesFS: os.DirFS("images"),
	}
	s.registerRoutes()
	return s
}

type Server struct {
	Mux *http.ServeMux
	db  *sql.DB
	kv  *bolt.DB

	imagesFS fs.FS
}

func (s *Server) registerRoutes() {
	image_service, _ := url.Parse("http://localhost:3000")
	s.Mux.Handle("/images/", httputil.NewSingleHostReverseProxy(image_service))

	s.Mux.HandleFunc("/dashboard/{id}", s.Dashboard)
	s.Mux.HandleFunc("/new_collection", s.NewCollection)
	s.Mux.Handle("/search", templ.Handler(SearchBox()))
	s.Mux.HandleFunc("/add_item/{id}", s.AddItem)

	s.Mux.HandleFunc("/login", s.Login)
	s.Mux.HandleFunc("/logout", s.Logout)
	s.Mux.HandleFunc("/signup", s.Signup)

	s.Mux.HandleFunc("/", s.Homepage)
}

func (s *Server) ListenAndServe(port string) {
	http.ListenAndServe(port, s.Mux)
}

func (s *Server) Close() {
	s.db.Close()
	s.kv.Close()
}

type UserItem struct {
	Name      string   `json:"name"`
	Metadata  string   `json:"metadata"`
	ImageRefs []string `json:"image_refs"`
}

type Collection struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (s *Server) Homepage(w http.ResponseWriter, r *http.Request) {
	activeUser, _ := s.GetUserFromRequest(r)
	if !activeUser {
		templ.Handler(Homepage()).ServeHTTP(w, r)
		return
	}
	http.Redirect(w, r, "/dashboard/0", http.StatusSeeOther)
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

	userItems := []UserItem{}

	hasQuery := r.URL.Query().Has("query")
	if hasQuery {
		u, _ := url.Parse(fmt.Sprintf("http://localhost:8083/search/%d/%d", userID, dbID))
		q := u.Query()
		q.Set("query", r.URL.Query().Get("query"))
		u.RawQuery = q.Encode()
		search_resp, err := http.Get(u.String())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer search_resp.Body.Close()
		b, _ := io.ReadAll(search_resp.Body)
		userResults := []struct {
			Name      string `json:"name"`
			Metadata  string `json:"metadata"`
			ImageRefs string `json:"image_refs"`
		}{}
		err = json.Unmarshal(b, &userResults)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		for _, result := range userResults {
			item := UserItem{Name: result.Name, Metadata: result.Metadata}
			json.Unmarshal([]byte(result.ImageRefs), &item.ImageRefs)
			userItems = append(userItems, item)
		}

	} else {
		rows, err = s.db.Query(fmt.Sprintf(`select name, metadata, image_refs from user_%d_collection_%d`, userID, dbID))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		for rows.Next() {
			row := UserItem{}
			refs := ""
			err := rows.Scan(&row.Name, &row.Metadata, &refs)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			_ = json.Unmarshal([]byte(refs), &row.ImageRefs)
			userItems = append(userItems, row)
		}
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
		templ.Handler(AddItem(r.URL.Path)).ServeHTTP(w, r)
		return
	}

	userID := -1
	row := s.db.QueryRow(`select id from users where username = ?`, username)
	if row.Scan(&userID) != nil {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}

	base64ImagesJSON := r.FormValue("photoData")
	var base64Images []ImageUploadImage
	err := json.Unmarshal([]byte(base64ImagesJSON), &base64Images)
	if err != nil {
		fmt.Println("Error parsing JSON:", err)
		return
	}

	// Upload the images to microservice a
	results, err := uploadBase64Images("http://localhost:3000", base64Images)
	if err != nil {
		fmt.Println("Error uploading images:", err)
		return
	}

	refs := []string{}
	for _, result := range results {
		refs = append(refs, result.URL)
		go ai_tagImage(result.URL)
	}

	image_refs, _ := json.Marshal(refs)

	name := r.FormValue("name")
	meta := r.FormValue("metadata")

	collectionID, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || collectionID < 0 {
		http.Error(w, "invalid collection id", http.StatusBadRequest)
		return
	}

	_, err = s.db.Exec(fmt.Sprintf(`insert into user_%d_collection_%d (name, metadata, image_refs) values (?, ?, ?)`, userID, collectionID), name, meta, image_refs)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/dashboard/%d", collectionID), http.StatusSeeOther)
}
