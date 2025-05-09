package main

import (
	"github.com/philippgille/chromem-go"
)

func Open(path string) (*chromem.DB, *chromem.Collection, error) {
	db, err := chromem.NewPersistentDB(path, true)
	if err != nil {
		return nil, nil, err
	}
	col, err := db.GetOrCreateCollection("image_descritions", nil, chromem.NewEmbeddingFuncOllama("nomic-embed-text", ""))
	return db, col, err
}
