package main

import (
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/a-h/templ"
	"golang.org/x/crypto/bcrypt"
)

func (s *Server) IsLoggedIn(r *http.Request) bool {
	cookie, err := r.Cookie("userTok")
	if err != nil {
		return false
	}
	found, _ := s.activeUser(cookie.Value)
	return found
}

func (s *Server) GetUserFromRequest(r *http.Request) (bool, string) {
	cookie, err := r.Cookie("userTok")
	if err != nil {
		return false, ""
	}
	return s.activeUser(cookie.Value)
}

func (s *Server) activeUser(token string) (bool, string) {
	// var found bool
	// var username string
	// s.kv.View(func(tx *bolt.Tx) error {
	// 	b := tx.Bucket([]byte("userSessions"))
	// 	u := b.Get([]byte(token))
	// 	if u == nil {
	// 		found = false
	// 	} else {
	// 		username = string(u)
	// 		found = true
	// 	}
	// 	return nil
	// })

	u, _ := url.Parse("http://localhost:8082/is_active")
	q := u.Query()
	q.Set("token", token)
	u.RawQuery = q.Encode()
	resp, err := http.Get(u.String())
	if err != nil {
		return false, ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, ""
	}
	var res = struct {
		Found    bool
		Username string
	}{
		Found:    false,
		Username: "",
	}
	b, _ := io.ReadAll(resp.Body)
	json.Unmarshal(b, &res)
	fmt.Println(string(b))
	fmt.Println(res)
	return res.Found, res.Username
}

func (s *Server) SetToken(token string, user string) {
	// s.kv.Update(func(tx *bolt.Tx) error {
	// 	b := tx.Bucket([]byte("userSessions"))
	// 	err := b.Put([]byte(token), []byte(user))
	// 	return err
	// })

	u, _ := url.Parse("http://localhost:8082/set_active")
	q := u.Query()
	q.Set("token", token)
	q.Set("user", user)
	u.RawQuery = q.Encode()
	http.Get(u.String())
}

func (s *Server) Logout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("userTok")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// s.kv.Update(func(tx *bolt.Tx) error {
	// 	b := tx.Bucket([]byte("userSessions"))
	// 	err := b.Delete([]byte(cookie.Value))
	// 	return err
	// })

	u, _ := url.Parse("http://localhost:8082/logout")
	q := u.Query()
	q.Set("token", cookie.Value)
	u.RawQuery = q.Encode()
	http.Get(u.String())

	http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
}

func (s *Server) Login(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		templ.Handler(Auth("Login")).ServeHTTP(w, r)
		return
	}
	if r.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	err := r.ParseForm()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	username := r.FormValue("email")
	password := r.FormValue("password")

	row := s.db.QueryRow(`select username, password from users where username = ?`, username)
	var hash string
	var dbUsername string
	if err := row.Scan(&dbUsername, &hash); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	err = bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h := sha512.New()
	h.Write([]byte(time.Now().String()))
	raw_id := h.Sum(nil)
	sessionID := hex.EncodeToString(raw_id)

	s.SetToken(sessionID, dbUsername)

	http.SetCookie(w, &http.Cookie{
		Name:     "userTok",
		Value:    sessionID,
		HttpOnly: true,
		//Secure:   true,
	})

	http.Redirect(w, r, "/dashboard/0", http.StatusTemporaryRedirect)
}

func (s *Server) Signup(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		templ.Handler(Auth("Signup")).ServeHTTP(w, r)
		return
	}
	if r.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	username := r.FormValue("email")
	password := r.FormValue("password")
	first_collection := r.FormValue("collection")

	row := s.db.QueryRow(`select username from users where username = ?`, username)
	var u string
	if err := row.Scan(&u); err == nil {
		http.Error(w, "user already exists", http.StatusForbidden)
		return
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_, err = s.db.Exec(`insert into users (username, password) values (?, ?)`, username, string(passwordHash))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 1. get user id
	row = s.db.QueryRow(`select id from users where username = ?`, username)
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
	_, err = s.db.Exec(fmt.Sprintf(`CREATE TABLE user_%d_collection_%d (id integer primary key autoincrement, name text, metadata text, image_refs text)`, id, max_collection_id))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 4. connect collection id to user id
	_, err = s.db.Exec(`insert into user_collections (collection_table_id, name, user_id) values (?, ?, ?)`, max_collection_id, first_collection, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h := sha512.New()
	h.Write([]byte(time.Now().String()))
	raw_id := h.Sum(nil)
	sessionID := hex.EncodeToString(raw_id)

	s.SetToken(sessionID, username)

	http.SetCookie(w, &http.Cookie{
		Name:     "userTok",
		Value:    sessionID,
		HttpOnly: true,
		//Secure:   true,
	})

	http.Redirect(w, r, "/dashboard/0", http.StatusTemporaryRedirect)

}
