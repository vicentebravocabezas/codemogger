package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/vicentebravocabezas/codemogger/codemogger"
)

var bgeHost = os.Getenv("HOST")

type embeddingResponse struct {
	Data []struct {
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

func Embeddings(prompt string) ([]float32, error) {
	reqBody := struct {
		Input string `json:"input"`
	}{
		Input: prompt,
	}

	rb, _ := json.Marshal(reqBody)

	res, err := http.Post(bgeHost, "application/json", bytes.NewReader(rb))
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	body, _ := io.ReadAll(res.Body)

	var r embeddingResponse

	if err := json.Unmarshal(body, &r); err != nil {
		return nil, err
	}

	if len(r.Data) == 0 {
		f, _ := os.OpenFile("b.json", os.O_CREATE|os.O_WRONLY, 0644)
		fmt.Fprintf(f, "%s\n", rb)
		f.Close()
		return nil, fmt.Errorf("no data returned. body: %s", body)
	}

	return r.Data[0].Embedding, nil
}

func embedder(_ context.Context, texts []string) ([][]float32, error) {
	vectors := make([][]float32, len(texts))
	for i := range texts {
		if texts[i] == "" {
			panic("empty text")
		}
		vec, err := Embeddings(texts[i])
		if err != nil {
			panic(err)
		}
		vectors[i] = vec
	}
	return vectors, nil
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: codemogger <directory>")
		os.Exit(1)
	}

	ctx := context.Background()

	dbPath, err := codemogger.ProjectDBPath(".")
	if err != nil {
		panic(err)
	}

	index, err := codemogger.New(codemogger.CodeIndexOptions{
		DBPath:   dbPath,
		Embedder: embedder,
	})
	if err != nil {
		panic(err)
	}
	defer index.Close()

	if _, err := index.Index(ctx, os.Args[1], nil); err != nil {
		panic(err)
	}

	fmt.Println("index done")

	results, err := index.Search(ctx, "authentication middleware", &codemogger.SearchOptions{
		Mode:  codemogger.SearchModeSemantic,
		Limit: 5,
	})
	if err != nil {
		panic(err)
	}

	for _, result := range results {
		fmt.Println(result.FilePath, result.Name, result.Score)
	}
}
