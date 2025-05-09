package main

import (
	"encoding/json"
	"fmt"
	"net/http"

	bolt "go.etcd.io/bbolt"
)

func main() {
	kv, err := bolt.Open("kv_bolt.db", 0600, nil)
	if err != nil {
		panic(err.Error())
	}

	kv.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte("userSessions"))
		return err
	})

	http.HandleFunc("/is_active", func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		fmt.Println("/is_active checking if:", token)

		var found bool
		var username string
		kv.View(func(tx *bolt.Tx) error {
			b := tx.Bucket([]byte("userSessions"))
			u := b.Get([]byte(token))
			if u == nil {
				found = false
			} else {
				username = string(u)
				found = true
			}
			return nil
		})
		json.NewEncoder(w).Encode(struct {
			Found    bool
			Username string
		}{
			Found:    found,
			Username: username,
		})
	})
	http.HandleFunc("/set_active", func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		user := r.URL.Query().Get("user")
		fmt.Println("/set_active setting token:", token, "for user:", user)

		kv.Update(func(tx *bolt.Tx) error {
			b := tx.Bucket([]byte("userSessions"))
			err := b.Put([]byte(token), []byte(user))
			return err
		})
		w.WriteHeader(http.StatusNoContent)
	})

	http.HandleFunc("/logout", func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		fmt.Println("/logout for token:", token)

		kv.Update(func(tx *bolt.Tx) error {
			b := tx.Bucket([]byte("userSessions"))
			err := b.Delete([]byte(token))
			return err
		})
		w.WriteHeader(http.StatusNoContent)
	})

	http.ListenAndServe(":8082", nil)
}
