// fakedrive imitates the Google OAuth device flow and the parts of Drive v3
// X-Panel uses, keeping files in memory. End-to-end tests only.
//
//	fakedrive -listen 127.0.0.1:9999
//
// GET /debug/files lists stored files; GET /debug/raw/<id> returns content.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

type file struct {
	ID, Name, Mime, Parent string
	Data                   []byte
}

var (
	mu       sync.Mutex
	files    = map[string]*file{}
	sessions = map[string]*file{}
	nextID   int
	polls    int
)

func id() string { nextID++; return fmt.Sprintf("f%d", nextID) }

func authed(r *http.Request) bool { return r.Header.Get("Authorization") == "Bearer access-1" }

func main() {
	listen := flag.String("listen", "127.0.0.1:9999", "")
	flag.Parse()
	nameRe := regexp.MustCompile(`name = '([^']*)'`)
	parentRe := regexp.MustCompile(`'([^']*)' in parents`)

	http.HandleFunc("/device/code", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"device_code":"dev-1","user_code":"ABCD-EFGH","verification_url":"https://www.google.com/device","expires_in":300,"interval":1}`)
	})
	http.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		mu.Lock()
		defer mu.Unlock()
		switch r.Form.Get("grant_type") {
		case "urn:ietf:params:oauth:grant-type:device_code":
			polls++
			if polls < 2 { // the user has not approved yet
				w.WriteHeader(428)
				fmt.Fprint(w, `{"error":"authorization_pending"}`)
				return
			}
			fmt.Fprint(w, `{"access_token":"access-1","refresh_token":"refresh-1","expires_in":3600}`)
		case "refresh_token":
			if r.Form.Get("refresh_token") != "refresh-1" {
				w.WriteHeader(400)
				fmt.Fprint(w, `{"error":"invalid_grant"}`)
				return
			}
			fmt.Fprint(w, `{"access_token":"access-1","expires_in":3600}`)
		}
	})
	http.HandleFunc("/revoke", func(w http.ResponseWriter, r *http.Request) {})
	http.HandleFunc("/drive/v3/about", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"user":{"emailAddress":"owner@example.com"}}`)
	})
	http.HandleFunc("/drive/v3/files", func(w http.ResponseWriter, r *http.Request) {
		if !authed(r) {
			w.WriteHeader(401)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if r.Method == http.MethodGet {
			q := r.URL.Query().Get("q")
			var out []map[string]string
			name, parent := "", ""
			if m := nameRe.FindStringSubmatch(q); m != nil {
				name = m[1]
			}
			if m := parentRe.FindStringSubmatch(q); m != nil {
				parent = m[1]
			}
			if strings.Contains(q, "mimeType != ") {
				// List of the files in a folder.
				for _, f := range files {
					if f.Parent == parent && f.Mime == "" {
						out = append(out, map[string]string{"id": f.ID, "name": f.Name, "size": strconv.Itoa(len(f.Data)), "createdTime": "2026-01-01T00:00:00Z"})
					}
				}
				json.NewEncoder(w).Encode(map[string]any{"files": out})
				return
			}
			for _, f := range files {
				if f.Name == name && f.Parent == parent && f.Mime != "" {
					out = append(out, map[string]string{"id": f.ID})
				}
			}
			json.NewEncoder(w).Encode(map[string]any{"files": out})
			return
		}
		var meta struct {
			Name     string   `json:"name"`
			MimeType string   `json:"mimeType"`
			Parents  []string `json:"parents"`
		}
		json.NewDecoder(r.Body).Decode(&meta)
		f := &file{ID: id(), Name: meta.Name, Mime: meta.MimeType}
		if len(meta.Parents) > 0 {
			f.Parent = meta.Parents[0]
		}
		files[f.ID] = f
		fmt.Fprintf(w, `{"id":%q}`, f.ID)
	})
	http.HandleFunc("/drive/v3/files/", func(w http.ResponseWriter, r *http.Request) {
		if !authed(r) {
			w.WriteHeader(401)
			return
		}
		fid := strings.TrimPrefix(r.URL.Path, "/drive/v3/files/")
		mu.Lock()
		f := files[fid]
		if r.Method == http.MethodDelete {
			delete(files, fid)
		}
		mu.Unlock()
		if f == nil {
			w.WriteHeader(404)
			return
		}
		if r.Method == http.MethodDelete {
			w.WriteHeader(204)
			return
		}
		w.Write(f.Data)
	})
	http.HandleFunc("/upload/drive/v3/files", func(w http.ResponseWriter, r *http.Request) {
		if !authed(r) {
			w.WriteHeader(401)
			return
		}
		var meta struct {
			Name    string   `json:"name"`
			Parents []string `json:"parents"`
		}
		json.NewDecoder(r.Body).Decode(&meta)
		mu.Lock()
		f := &file{ID: id(), Name: meta.Name}
		if len(meta.Parents) > 0 {
			f.Parent = meta.Parents[0]
		}
		sid := "s" + f.ID
		sessions[sid] = f
		mu.Unlock()
		w.Header().Set("Location", "http://"+r.Host+"/session/"+sid)
	})
	http.HandleFunc("/session/", func(w http.ResponseWriter, r *http.Request) {
		if !authed(r) {
			w.WriteHeader(401)
			return
		}
		sid := strings.TrimPrefix(r.URL.Path, "/session/")
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		defer mu.Unlock()
		f := sessions[sid]
		if f == nil {
			w.WriteHeader(404)
			return
		}
		f.Data = append(f.Data, b...)
		if strings.HasSuffix(r.Header.Get("Content-Range"), "/*") {
			w.WriteHeader(308)
			return
		}
		files[f.ID] = f
		delete(sessions, sid)
		fmt.Fprintf(w, `{"id":%q,"name":%q,"size":"%d"}`, f.ID, f.Name, len(f.Data))
	})
	http.HandleFunc("/debug/files", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		var out []map[string]string
		for _, f := range files {
			if f.Mime == "" {
				out = append(out, map[string]string{"id": f.ID, "name": f.Name, "size": strconv.Itoa(len(f.Data))})
			}
		}
		json.NewEncoder(w).Encode(out)
	})
	http.HandleFunc("/debug/raw/", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		f := files[strings.TrimPrefix(r.URL.Path, "/debug/raw/")]
		mu.Unlock()
		if f == nil {
			w.WriteHeader(404)
			return
		}
		w.Write(f.Data)
	})
	log.Fatal(http.ListenAndServe(*listen, nil))
}
