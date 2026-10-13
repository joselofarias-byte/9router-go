package multiagent

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

func decodeRequest(w http.ResponseWriter, r *http.Request) (Request, error) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req Request
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil { return req, err }
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF { return req, errors.New("trailing JSON") }
	if strings.TrimSpace(req.Prompt) == "" || len(req.Prompt) > 20000 ||
		len(req.Models) == 0 || len(req.Models) > 8 ||
		req.Concurrency < 0 || req.Concurrency > 8 || req.TimeoutMS < 0 || req.TimeoutMS > 120000 {
		return req, errors.New("invalid limits")
	}
	seen := make(map[string]bool, len(req.Models))
	for _, model := range req.Models {
		if strings.TrimSpace(model) == "" || model != strings.TrimSpace(model) || seen[model] {
			return req, errors.New("invalid or duplicate model")
		}
		seen[model] = true
	}
	return req, nil
}
