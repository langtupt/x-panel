package main

import (
	"encoding/json"
	"net/http"
	"strings"
)

// A fake OpenAI chat completions endpoint for malware.sh: files containing
// "XP-INJECTED" are legitimate files with injected lines (cleaned version =
// those lines removed), "XP-SHELL" is a webshell, anything else is clean.
func init() {
	http.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-test-0123456789abcdefghij" {
			w.WriteHeader(401)
			w.Write([]byte(`{"error":{"message":"Incorrect API key provided"}}`))
			return
		}
		var req struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		user := req.Messages[len(req.Messages)-1].Content
		file := user
		if i := strings.Index(user, "<<<FILE\n"); i >= 0 {
			file = strings.TrimSuffix(user[i+8:], "\nFILE>>>")
		}
		v := map[string]any{"verdict": "clean", "confidence": 0.9, "summary": "Mã hợp lệ.", "injected": false, "cleaned_content": nil}
		switch {
		case strings.Contains(file, "XP-INJECTED"):
			var keep []string
			for _, l := range strings.SplitAfter(file, "\n") {
				if !strings.Contains(l, "XP-INJECTED") {
					keep = append(keep, l)
				}
			}
			v = map[string]any{"verdict": "malicious", "confidence": 0.95, "summary": "File hợp lệ bị chèn mã độc.",
				"injected": true, "cleaned_content": strings.Join(keep, "")}
		case strings.Contains(file, "XP-SHELL"):
			v = map[string]any{"verdict": "malicious", "confidence": 0.99, "summary": "Webshell.", "injected": false, "cleaned_content": nil}
		}
		content, _ := json.Marshal(v)
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": string(content)}}}})
	})
}
