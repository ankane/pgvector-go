package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func main() {
	apiKey := os.Getenv("CO_API_KEY")
	if apiKey == "" {
		fmt.Println("Set CO_API_KEY")
		os.Exit(1)
	}

	ctx := context.Background()

	conn, err := pgx.Connect(ctx, "postgres://localhost/pgvector_example")
	if err != nil {
		panic(err)
	}
	defer conn.Close(ctx)

	_, err = conn.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS vector")
	if err != nil {
		panic(err)
	}

	_, err = conn.Exec(ctx, "DROP TABLE IF EXISTS documents")
	if err != nil {
		panic(err)
	}

	_, err = conn.Exec(ctx, "CREATE TABLE documents (id bigserial PRIMARY KEY, content text, embedding bit(1024))")
	if err != nil {
		panic(err)
	}

	input := []string{
		"The dog is barking",
		"The cat is purring",
		"The bear is growling",
	}
	embeddings, err := Embed(input, "search_document", apiKey)
	if err != nil {
		panic(err)
	}

	for i, content := range input {
		_, err := conn.Exec(ctx, "INSERT INTO documents (content, embedding) VALUES ($1, $2)", content, embeddings[i])
		if err != nil {
			panic(err)
		}
	}

	query := "forest"
	queryEmbedding, err := Embed([]string{query}, "search_query", apiKey)
	if err != nil {
		panic(err)
	}

	rows, err := conn.Query(ctx, "SELECT id, content FROM documents ORDER BY embedding <~> $1 LIMIT 5", queryEmbedding[0])
	if err != nil {
		panic(err)
	}
	defer rows.Close()

	for rows.Next() {
		var id int64
		var content string
		err = rows.Scan(&id, &content)
		if err != nil {
			panic(err)
		}
		fmt.Println(id, content)
	}

	if rows.Err() != nil {
		panic(rows.Err())
	}
}

type embedRequest struct {
	Texts          []string `json:"texts"`
	Model          string   `json:"model"`
	InputType      string   `json:"input_type"`
	EmbeddingTypes []string `json:"embedding_types"`
}

func Embed(texts []string, inputType string, apiKey string) ([]pgtype.Bits, error) {
	url := "https://api.cohere.com/v1/embed"
	data := &embedRequest{
		Texts:          texts,
		Model:          "embed-english-v3.0",
		InputType:      inputType,
		EmbeddingTypes: []string{"ubinary"},
	}

	b, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(b))
	if err != nil {
		return nil, err
	}

	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", apiKey))
	req.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Bad status code: %d", resp.StatusCode)
	}

	var result map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		return nil, err
	}

	var embeddings []pgtype.Bits
	for _, item := range result["embeddings"].(map[string]interface{})["ubinary"].([]interface{}) {
		buf := make([]byte, 0, len(item.([]interface{})))
		for _, v := range item.([]interface{}) {
			buf = append(buf, uint8(v.(float64)))
		}
		embedding := pgtype.Bits{Bytes: buf, Len: int32(len(buf) * 8), Valid: true}
		embeddings = append(embeddings, embedding)
	}
	return embeddings, nil
}
